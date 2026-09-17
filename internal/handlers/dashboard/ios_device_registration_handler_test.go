package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"xprem/internal/services"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicIosDeviceRoutesAreInvalidLinksInStatelessMode(t *testing.T) {
	t.Setenv("BASE_URL", "https://ota.example.com")
	handler := NewIosCredentialsHandler(services.NewIosCredentialsService(nil, nil))
	router := mux.NewRouter()
	router.HandleFunc("/device-registrations/{TOKEN}", handler.PublicIosDeviceInvitationHandler).Methods(http.MethodGet)
	router.HandleFunc("/device-registrations/{TOKEN}/profile", handler.IosDeviceRegistrationProfileHandler).Methods(http.MethodGet)
	router.HandleFunc("/device-registrations/{TOKEN}/enroll", handler.EnrollIosDeviceHandler).Methods(http.MethodPost)
	router.HandleFunc("/device-registrations/{TOKEN}/registrations/{REGISTRATION_ID}", handler.PublicIosDeviceRegistrationHandler).Methods(http.MethodGet)
	token := strings.Repeat("A", 43)

	for _, path := range []string{"", "/profile", "/registrations/11111111-1111-1111-1111-111111111111"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/device-registrations/"+token+path, nil))
		assert.Equal(t, http.StatusNotFound, recorder.Code, path)
		assert.JSONEq(t, `{"error":"invalid-link"}`, recorder.Body.String(), path)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/device-registrations/"+token+"/enroll", strings.NewReader("device response")))
	require.Equal(t, http.StatusMovedPermanently, recorder.Code)
	assert.Equal(t, "https://ota.example.com/dashboard/register-device/"+token+"?error=invalid-link", recorder.Header().Get("Location"))
}
