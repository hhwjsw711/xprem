package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"xprem/config"
	"xprem/internal/handlers"
	"xprem/internal/services"
	"xprem/internal/store"
	"xprem/internal/validation"

	"github.com/gorilla/mux"
)

const maxIosDeviceResponseBytes = 64 << 10

func iosDeviceRegistrationPageURL(token string) string {
	return config.BaseURL() + "/dashboard/register-device/" + url.PathEscape(token)
}

func (h *IosCredentialsHandler) CreateIosDeviceInvitationHandler(w http.ResponseWriter, r *http.Request) {
	var requestBody struct {
		Label          string `json:"label"`
		ExpiresInHours int    `json:"expiresInHours"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&requestBody); err != nil && !errors.Is(err, io.EOF) {
		handlers.RenderError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	invitation, token, err := h.iosCredentialsService.CreateIosDeviceInvitation(r.Context(), mux.Vars(r)["APP_ID"], requestBody.Label, requestBody.ExpiresInHours)
	if err != nil {
		renderIosServiceError(w, err, "An internal error occurred while creating the iPhone registration link.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	handlers.RenderJSON(w, http.StatusCreated, map[string]any{"invitation": invitation, "url": iosDeviceRegistrationPageURL(token)})
}

func (h *IosCredentialsHandler) ListIosDeviceInvitationsHandler(w http.ResponseWriter, r *http.Request) {
	invitations, err := h.iosCredentialsService.ListIosDeviceInvitations(r.Context(), mux.Vars(r)["APP_ID"])
	if err != nil {
		renderIosServiceError(w, err, "An internal error occurred while listing iPhone registration links.")
		return
	}
	renderJSON(w, map[string]any{"invitations": invitations})
}

func (h *IosCredentialsHandler) RevokeIosDeviceInvitationHandler(w http.ResponseWriter, r *http.Request) {
	invitationId := uuidVar(w, r, "INVITATION_ID", "invitation id")
	if invitationId == "" {
		return
	}
	if err := h.iosCredentialsService.RevokeIosDeviceInvitation(r.Context(), mux.Vars(r)["APP_ID"], invitationId); err != nil {
		renderIosServiceError(w, err, "An internal error occurred while revoking the iPhone registration link.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *IosCredentialsHandler) DisableAppleDeviceHandler(w http.ResponseWriter, r *http.Request) {
	h.setAppleDeviceEnabled(w, r, false)
}

func (h *IosCredentialsHandler) EnableAppleDeviceHandler(w http.ResponseWriter, r *http.Request) {
	h.setAppleDeviceEnabled(w, r, true)
}

func (h *IosCredentialsHandler) setAppleDeviceEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if err := h.iosCredentialsService.SetAppleDeviceEnabled(r.Context(), mux.Vars(r)["APP_ID"], mux.Vars(r)["DEVICE_ID"], enabled); err != nil {
		renderIosServiceError(w, err, "An internal error occurred while updating the Apple device.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *IosCredentialsHandler) ListAppleDevicesHandler(w http.ResponseWriter, r *http.Request) {
	devices, err := h.iosCredentialsService.ListAppleDevices(r.Context(), mux.Vars(r)["APP_ID"])
	if err != nil {
		renderIosServiceError(w, err, "An internal error occurred while listing Apple devices.")
		return
	}
	renderJSON(w, map[string]any{"devices": devices})
}

func setPublicIosDeviceHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

func isInvalidIosDeviceLink(err error) bool {
	var notFound *store.ErrResourceNotFound
	return errors.As(err, &notFound) || errors.Is(err, store.ErrNotSupportedInStatelessMode)
}

// renderPublicIosDeviceError answers unknown, expired and revoked links identically.
func renderPublicIosDeviceError(w http.ResponseWriter, err error) {
	if errors.Is(err, services.ErrIosDeviceInvitationUsed) {
		handlers.RenderJSON(w, http.StatusGone, map[string]string{"error": "used"})
		return
	}
	if isInvalidIosDeviceLink(err) {
		handlers.RenderJSON(w, http.StatusNotFound, map[string]string{"error": "invalid-link"})
		return
	}
	log.Printf("ios device registration link failed: %v", err)
	handlers.RenderError(w, http.StatusInternalServerError, "An internal error occurred.")
}

func (h *IosCredentialsHandler) PublicIosDeviceInvitationHandler(w http.ResponseWriter, r *http.Request) {
	setPublicIosDeviceHeaders(w)
	invitation, err := h.iosCredentialsService.GetPublicIosDeviceInvitation(r.Context(), mux.Vars(r)["TOKEN"])
	if err != nil {
		renderPublicIosDeviceError(w, err)
		return
	}
	handlers.RenderJSON(w, http.StatusOK, invitation)
}

func (h *IosCredentialsHandler) IosDeviceRegistrationProfileHandler(w http.ResponseWriter, r *http.Request) {
	setPublicIosDeviceHeaders(w)
	token := mux.Vars(r)["TOKEN"]
	enrollURL := config.BaseURL() + "/device-registrations/" + url.PathEscape(token) + "/enroll"
	profile, err := h.iosCredentialsService.IosDeviceRegistrationProfile(r.Context(), token, enrollURL)
	if err != nil {
		renderPublicIosDeviceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-apple-aspen-config")
	w.Header().Set("Content-Disposition", `attachment; filename="register-iphone.mobileconfig"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(profile)
}

// EnrollIosDeviceHandler receives the attributes iOS posts after the profile is installed and
// always sends Safari back to the registration page.
func (h *IosCredentialsHandler) EnrollIosDeviceHandler(w http.ResponseWriter, r *http.Request) {
	setPublicIosDeviceHeaders(w)
	token := mux.Vars(r)["TOKEN"]
	target := iosDeviceRegistrationPageURL(token)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxIosDeviceResponseBytes))
	registrationId := ""
	if err == nil {
		registrationId, err = h.iosCredentialsService.EnrollIosDevice(r.Context(), token, body)
	}
	var maxBytesErr *http.MaxBytesError
	used := errors.Is(err, services.ErrIosDeviceInvitationUsed)
	if err != nil && !used && !isInvalidIosDeviceLink(err) && !validation.IsValidationError(err) && !errors.As(err, &maxBytesErr) {
		log.Printf("ios device enrollment failed: %v", err)
	}
	switch {
	case used:
		target += "?error=used"
	case err != nil:
		target += "?error=invalid-link"
	default:
		target += "?registration=" + url.QueryEscape(registrationId)
	}
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusMovedPermanently)
}

func (h *IosCredentialsHandler) PublicIosDeviceRegistrationHandler(w http.ResponseWriter, r *http.Request) {
	setPublicIosDeviceHeaders(w)
	registration, err := h.iosCredentialsService.GetPublicIosDeviceRegistration(r.Context(), mux.Vars(r)["TOKEN"], mux.Vars(r)["REGISTRATION_ID"])
	if err != nil {
		renderPublicIosDeviceError(w, err)
		return
	}
	handlers.RenderJSON(w, http.StatusOK, registration)
}
