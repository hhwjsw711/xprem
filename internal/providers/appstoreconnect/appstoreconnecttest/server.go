// Package appstoreconnecttest runs an in-memory App Store Connect API that
// verifies request tokens, for tests.
package appstoreconnecttest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	KeyID    = "ABC123DEFG"
	IssuerID = "57246542-96fe-1a63-e053-0824d011072a"
	TeamID   = "ABCDE12345"
)

// Server is a fake App Store Connect team reachable at BaseURL.
type Server struct {
	BaseURL       string
	PrivateKeyPEM string

	mu sync.Mutex
	// Status, when set, answers every authenticated request with that status.
	Status int
	// DeviceLimitReached makes device registration answer 409.
	DeviceLimitReached bool
	// DeviceConflictDetail injects a registration conflict while lookups still return no device.
	DeviceConflictDetail string
	// ConflictThenAppear makes device registration answer 409 while adding the device to the team.
	ConflictThenAppear bool
	// CertificateLimitReached makes certificate creation answer 409.
	CertificateLimitReached bool
	// AfterCertificateCreated runs once Apple holds a new certificate, before the answer is sent.
	AfterCertificateCreated func()
	Devices                 []Device
	BundleIDs               []BundleID
	Profiles                []Profile
	requests                []string
	certificates            map[string][]byte
	certificatesCreated     int
	publicKey               *ecdsa.PublicKey
	issuer                  *ecdsa.PrivateKey
}

// Device is a registered device with the attributes Apple lists.
type Device struct {
	ID, Name, UDID, Model, Platform, DeviceClass, Status, AddedDate string
}

// BundleID is a registered app identifier.
type BundleID struct {
	ID, Identifier, Name string
}

// Profile is a provisioning profile with the relationships Apple records.
type Profile struct {
	ID, Name, Type, State, BundleID string
	CertificateIDs, DeviceIDs       []string
	Content                         []byte
}

func New(t testing.TB) *Server {
	t.Helper()
	apiKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(apiKey)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})),
		certificates:  map[string][]byte{},
		publicKey:     &apiKey.PublicKey,
		issuer:        issuer,
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	server.BaseURL = httpServer.URL + "/v1"
	return server
}

// RegisterCertificate adds an existing certificate to the team's distribution certificates.
func (s *Server) RegisterCertificate(der []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addCertificate(der)
}

// RequestCount counts the requests matching "METHOD /path".
func (s *Server) RequestCount(request string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, received := range s.requests {
		if received == request {
			count++
		}
	}
	return count
}

func (s *Server) addCertificate(der []byte) string {
	s.certificatesCreated++
	id := fmt.Sprintf("CERT%d", s.certificatesCreated)
	s.certificates[id] = der
	return id
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	if err := s.authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, "NOT_AUTHORIZED", err.Error())
		return
	}
	if s.Status != 0 {
		writeError(w, s.Status, "FAILURE", "injected failure")
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /v1/bundleIds":
		s.listBundleIDs(w, r)
	case "POST /v1/bundleIds":
		s.createBundleID(w, r)
	case "GET /v1/certificates":
		s.listCertificates(w, r)
	case "POST /v1/certificates":
		s.createCertificate(w, r)
	case "GET /v1/devices":
		s.listDevices(w, r)
	case "POST /v1/devices":
		s.createDevice(w, r)
	case "GET /v1/profiles":
		s.listProfiles(w, r)
	case "POST /v1/profiles":
		s.createProfile(w, r)
	default:
		s.serveResource(w, r)
	}
}

// serveResource routes the requests that carry a resource id in their path.
func (s *Server) serveResource(w http.ResponseWriter, r *http.Request) {
	if id, ok := strings.CutPrefix(r.URL.Path, "/v1/devices/"); ok && r.Method == http.MethodPatch {
		s.updateDevice(w, r, id)
		return
	}
	if id, ok := strings.CutPrefix(r.URL.Path, "/v1/certificates/"); ok && r.Method == http.MethodDelete {
		if _, exists := s.certificates[id]; !exists {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "There is no resource of type 'certificates' with id '"+id+"'")
			return
		}
		delete(s.certificates, id)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if rest, ok := strings.CutPrefix(r.URL.Path, "/v1/profiles/"); ok {
		id, relationship, _ := strings.Cut(rest, "/relationships/")
		index := slices.IndexFunc(s.Profiles, func(profile Profile) bool { return profile.ID == id })
		if index < 0 {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "There is no resource of type 'profiles' with id '"+id+"'")
			return
		}
		switch {
		case r.Method == http.MethodDelete && relationship == "":
			s.Profiles = slices.Delete(s.Profiles, index, index+1)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && relationship == "certificates":
			writeJSON(w, http.StatusOK, map[string]any{"data": refs("certificates", s.Profiles[index].CertificateIDs)})
		case r.Method == http.MethodGet && relationship == "devices":
			writeJSON(w, http.StatusOK, map[string]any{"data": refs("devices", s.Profiles[index].DeviceIDs)})
		default:
			writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown resource")
		}
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown resource")
}

func refs(kind string, ids []string) []map[string]string {
	data := []map[string]string{}
	for _, id := range ids {
		data = append(data, map[string]string{"type": kind, "id": id})
	}
	return data
}

func (s *Server) listBundleIDs(w http.ResponseWriter, r *http.Request) {
	identifier := r.URL.Query().Get("filter[identifier]")
	data := []map[string]any{}
	for _, bundleID := range s.BundleIDs {
		if identifier == "" || strings.Contains(bundleID.Identifier, identifier) {
			data = append(data, map[string]any{"type": "bundleIds", "id": bundleID.ID, "attributes": map[string]string{
				"identifier": bundleID.Identifier, "name": bundleID.Name, "platform": "IOS",
			}})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) createBundleID(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Data struct {
			Attributes struct {
				Identifier string `json:"identifier"`
				Name       string `json:"name"`
				Platform   string `json:"platform"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "PARAMETER_ERROR", err.Error())
		return
	}
	attributes := body.Data.Attributes
	if attributes.Identifier == "" || attributes.Name == "" || attributes.Platform != "IOS" {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "invalid bundle id attributes")
		return
	}
	for _, bundleID := range s.BundleIDs {
		if bundleID.Identifier == attributes.Identifier {
			writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "An App ID with Identifier '"+attributes.Identifier+"' is not available. Please enter a different string.")
			return
		}
	}
	bundleID := BundleID{ID: fmt.Sprintf("BUNDLE%d", len(s.BundleIDs)+1), Identifier: attributes.Identifier, Name: attributes.Name}
	s.BundleIDs = append(s.BundleIDs, bundleID)
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"type": "bundleIds", "id": bundleID.ID, "attributes": map[string]string{
		"identifier": bundleID.Identifier, "name": bundleID.Name, "platform": "IOS",
	}}})
}

// createCertificate signs the request's CSR the way Apple names iOS distribution certificates.
func (s *Server) createCertificate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Data struct {
			Attributes struct {
				CsrContent      string `json:"csrContent"`
				CertificateType string `json:"certificateType"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "PARAMETER_ERROR", err.Error())
		return
	}
	block, _ := pem.Decode([]byte(body.Data.Attributes.CsrContent))
	if block == nil || block.Type != "CERTIFICATE REQUEST" || body.Data.Attributes.CertificateType != "IOS_DISTRIBUTION" {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "invalid certificate attributes")
		return
	}
	request, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || request.CheckSignature() != nil {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "invalid certificate signing request")
		return
	}
	if s.CertificateLimitReached {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "You already have a current iOS Distribution certificate or a pending certificate request.")
		return
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(int64(s.certificatesCreated + 1)),
		Subject:      pkix.Name{CommonName: "iPhone Distribution: Example Team (" + TeamID + ")", OrganizationalUnit: []string{TeamID}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, request.PublicKey, s.issuer)
	if err != nil {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", err.Error())
		return
	}
	id := s.addCertificate(der)
	if s.AfterCertificateCreated != nil {
		s.AfterCertificateCreated()
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"type": "certificates", "id": id, "attributes": map[string]any{"certificateContent": der}}})
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("filter[name]")
	data := []map[string]any{}
	for _, profile := range s.Profiles {
		if name == "" || strings.Contains(profile.Name, name) {
			data = append(data, profileJSON(profile))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func profileJSON(profile Profile) map[string]any {
	return map[string]any{"type": "profiles", "id": profile.ID, "attributes": map[string]any{
		"name": profile.Name, "profileType": profile.Type, "profileState": profile.State, "profileContent": profile.Content,
		"expirationDate": time.Now().Add(365 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000-0700"),
	}}
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Data struct {
			Attributes struct {
				Name        string `json:"name"`
				ProfileType string `json:"profileType"`
			} `json:"attributes"`
			Relationships struct {
				BundleID     struct{ Data ref }   `json:"bundleId"`
				Certificates struct{ Data []ref } `json:"certificates"`
				Devices      struct{ Data []ref } `json:"devices"`
			} `json:"relationships"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "PARAMETER_ERROR", err.Error())
		return
	}
	attributes, relationships := body.Data.Attributes, body.Data.Relationships
	if attributes.Name == "" || (attributes.ProfileType != "IOS_APP_STORE" && attributes.ProfileType != "IOS_APP_ADHOC") {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "invalid profile attributes")
		return
	}
	for _, profile := range s.Profiles {
		if profile.Name == attributes.Name {
			writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "The provided entity includes an attribute with a value that has already been used")
			return
		}
	}
	if !slices.ContainsFunc(s.BundleIDs, func(bundleID BundleID) bool { return bundleID.ID == relationships.BundleID.Data.ID }) {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.RELATIONSHIP.INVALID", "There is no bundle id with id '"+relationships.BundleID.Data.ID+"'")
		return
	}
	if len(relationships.Certificates.Data) == 0 {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.RELATIONSHIP.INVALID", "A profile needs at least one certificate")
		return
	}
	profile := Profile{
		ID: fmt.Sprintf("PROFILE%d", len(s.Profiles)+1), Name: attributes.Name, Type: attributes.ProfileType,
		State: "ACTIVE", BundleID: relationships.BundleID.Data.ID, CertificateIDs: []string{}, DeviceIDs: []string{},
	}
	for _, certificate := range relationships.Certificates.Data {
		if _, exists := s.certificates[certificate.ID]; !exists {
			writeError(w, http.StatusConflict, "ENTITY_ERROR.RELATIONSHIP.INVALID", "There is no certificate with id '"+certificate.ID+"'")
			return
		}
		profile.CertificateIDs = append(profile.CertificateIDs, certificate.ID)
	}
	for _, device := range relationships.Devices.Data {
		if !slices.ContainsFunc(s.Devices, func(known Device) bool { return known.ID == device.ID }) {
			writeError(w, http.StatusConflict, "ENTITY_ERROR.RELATIONSHIP.INVALID", "There is no device with id '"+device.ID+"'")
			return
		}
		profile.DeviceIDs = append(profile.DeviceIDs, device.ID)
	}
	if attributes.ProfileType == "IOS_APP_ADHOC" && len(profile.DeviceIDs) == 0 {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.RELATIONSHIP.INVALID", "An Ad Hoc profile needs at least one device")
		return
	}
	profile.Content = []byte("mobileprovision " + profile.ID + " " + strings.Join(profile.DeviceIDs, ","))
	s.Profiles = append(s.Profiles, profile)
	writeJSON(w, http.StatusCreated, map[string]any{"data": profileJSON(profile)})
}

type ref struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (s *Server) authorize(r *http.Request) error {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return fmt.Errorf("missing bearer token")
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		return s.publicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}), jwt.WithAudience("appstoreconnect-v1"), jwt.WithIssuer(IssuerID), jwt.WithIssuedAt(), jwt.WithExpirationRequired())
	if err != nil {
		return err
	}
	if token.Header["kid"] != KeyID || token.Header["typ"] != "JWT" {
		return fmt.Errorf("unexpected token header %v", token.Header)
	}
	issuedAt, _ := claims.GetIssuedAt()
	expiresAt, _ := claims.GetExpirationTime()
	if issuedAt == nil || expiresAt.Sub(issuedAt.Time) > 20*time.Minute {
		return fmt.Errorf("token lifetime exceeds 20 minutes")
	}
	return nil
}

// RegisteredDevices returns the devices of the fake team.
func (s *Server) RegisteredDevices() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Device(nil), s.Devices...)
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	status, udid := r.URL.Query().Get("filter[status]"), r.URL.Query().Get("filter[udid]")
	data := []map[string]any{}
	for _, device := range s.Devices {
		if (status == "" || device.Status == status) && (udid == "" || device.UDID == udid) {
			data = append(data, map[string]any{"type": "devices", "id": device.ID, "attributes": map[string]string{
				"name":        device.Name,
				"udid":        device.UDID,
				"model":       device.Model,
				"platform":    device.Platform,
				"deviceClass": device.DeviceClass,
				"status":      device.Status,
				"addedDate":   device.AddedDate,
			}})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Data struct {
			Attributes struct {
				Name     string `json:"name"`
				Platform string `json:"platform"`
				UDID     string `json:"udid"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "PARAMETER_ERROR", err.Error())
		return
	}
	attributes := body.Data.Attributes
	if s.DeviceConflictDetail != "" {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", s.DeviceConflictDetail)
		return
	}
	if attributes.Name == "" || len([]rune(attributes.Name)) > 50 || attributes.Platform != "IOS" || attributes.UDID == "" {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "invalid device attributes")
		return
	}
	for _, device := range s.Devices {
		if device.UDID == attributes.UDID {
			writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "A device with number '"+attributes.UDID+"' already exists on this team.")
			return
		}
	}
	if s.DeviceLimitReached {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "There are no current ios devices on this team matching the provided device IDs. You have reached the maximum number of registered iPhone devices.")
		return
	}
	device := Device{
		ID:          fmt.Sprintf("DEVICE%d", len(s.Devices)+1),
		Name:        attributes.Name,
		UDID:        attributes.UDID,
		Platform:    "IOS",
		DeviceClass: "IPHONE",
		Status:      "ENABLED",
		AddedDate:   time.Now().UTC().Format("2006-01-02T15:04:05.000-0700"),
	}
	s.Devices = append(s.Devices, device)
	if s.ConflictThenAppear {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "A device with number '"+attributes.UDID+"' already exists on this team.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"type": "devices", "id": device.ID, "attributes": attributes}})
}

func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				Status string `json:"status"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Data.Type != "devices" || body.Data.ID != id {
		writeError(w, http.StatusConflict, "ENTITY_ERROR", "invalid device update")
		return
	}
	status := body.Data.Attributes.Status
	if status != "ENABLED" && status != "DISABLED" {
		writeError(w, http.StatusConflict, "ENTITY_ERROR.ATTRIBUTE.INVALID", "invalid status")
		return
	}
	for i, device := range s.Devices {
		if device.ID == id {
			s.Devices[i].Status = status
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"type": "devices", "id": id, "attributes": map[string]string{
				"name": device.Name, "udid": device.UDID, "platform": device.Platform, "deviceClass": device.DeviceClass, "status": status,
			}}})
			return
		}
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "There is no resource of type 'devices' with id '"+id+"'")
}

func (s *Server) listCertificates(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("filter[certificateType]") != "DISTRIBUTION,IOS_DISTRIBUTION" {
		writeError(w, http.StatusBadRequest, "PARAMETER_ERROR", "unexpected certificate filter")
		return
	}
	data := []map[string]any{}
	for id, der := range s.certificates {
		data = append(data, map[string]any{"type": "certificates", "id": id, "attributes": map[string]any{"certificateContent": der}})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string, detail string) {
	writeJSON(w, status, map[string]any{"errors": []map[string]string{{"status": fmt.Sprint(status), "code": code, "title": "Request failed", "detail": detail}}})
}
