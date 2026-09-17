package infrastructure

import (
	"net/http"
	"xprem/internal/middleware"

	"github.com/gorilla/mux"
)

// registerLinkRoutes registers what an anonymous holder of a token link can
// reach: a shared build and the iPhone registration flow.
func registerLinkRoutes(r *mux.Router, container *AppContainer) {
	r.HandleFunc("/build-shares/{TOKEN}", container.BuildRegistryHandler.PublicShare).Methods(http.MethodGet)
	r.HandleFunc("/build-shares/{TOKEN}/manifest.plist", container.BuildRegistryHandler.PublicShareManifest).Methods(http.MethodGet)
	r.HandleFunc("/build-shares/{TOKEN}/app.ipa", container.BuildRegistryHandler.PublicShareArtifact).Methods(http.MethodGet)

	deviceRegistrations := r.PathPrefix("/device-registrations").Subrouter()
	deviceRegistrations.Use(middleware.NewDashboardCORSMiddleware())
	deviceRegistrations.Use(middleware.NewReadDeadlineMiddleware(authReadDeadline))
	deviceRegistrations.PathPrefix("/").HandlerFunc(func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodOptions)
	deviceRegistrations.HandleFunc("/{TOKEN}", container.IosCredentialsHandler.PublicIosDeviceInvitationHandler).Methods(http.MethodGet)
	deviceRegistrations.HandleFunc("/{TOKEN}/profile", container.IosCredentialsHandler.IosDeviceRegistrationProfileHandler).Methods(http.MethodGet)
	deviceRegistrations.HandleFunc("/{TOKEN}/enroll", container.IosCredentialsHandler.EnrollIosDeviceHandler).Methods(http.MethodPost)
	deviceRegistrations.HandleFunc("/{TOKEN}/registrations/{REGISTRATION_ID}", container.IosCredentialsHandler.PublicIosDeviceRegistrationHandler).Methods(http.MethodGet)
}
