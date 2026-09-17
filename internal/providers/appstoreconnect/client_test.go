package appstoreconnect

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"xprem/internal/providers/appstoreconnect/appstoreconnecttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientFollowsListPages checks aggregation, filtering and absolute pagination links.
func TestClientFollowsListPages(t *testing.T) {
	for _, endpoint := range []string{"devices", "certificates"} {
		t.Run(endpoint, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, "/v1/"+endpoint, r.URL.Path)
				assert.Equal(t, "200", r.URL.Query().Get("limit"))
				if endpoint == "certificates" {
					assert.Equal(t, "DISTRIBUTION,IOS_DISTRIBUTION", r.URL.Query().Get("filter[certificateType]"))
				}
				page := r.URL.Query().Get("cursor")
				next := ""
				if page == "" {
					query := r.URL.Query()
					query.Set("cursor", "page-2")
					next = "http://" + r.Host + r.URL.Path + "?" + query.Encode()
				}
				attributes := map[string]any{"platform": "IOS", "certificateContent": []byte("certificate-" + page)}
				entries := []map[string]any{{"id": "id-" + page, "attributes": attributes}}
				if endpoint == "devices" && page != "" {
					entries = append(entries, map[string]any{"id": "mac", "attributes": map[string]string{"platform": "MAC_OS"}})
				}
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": entries, "links": map[string]string{"next": next}}))
			}))
			defer server.Close()
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			require.NoError(t, err)
			client := NewClient(server.URL+"/v1", "key", "issuer", key)
			if endpoint == "devices" {
				devices, err := client.ListIOSDevices(context.Background())
				require.NoError(t, err)
				require.Len(t, devices, 2)
				assert.Equal(t, "id-page-2", devices[1].ID)
			} else {
				certificates, err := client.ListDistributionCertificates(context.Background())
				require.NoError(t, err)
				require.Len(t, certificates, 2)
				assert.Equal(t, []byte("certificate-page-2"), certificates[1].DER)
			}
			assert.Equal(t, 2, requests)
		})
	}
}

// TestClientRejectsUnsafePagination prevents bearer tokens leaking through next links.
func TestClientRejectsUnsafePagination(t *testing.T) {
	for _, next := range []string{"https://other.example/v1/devices", "//other.example/v1/devices", "/v1/devices?limit=200"} {
		t.Run(next, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"data":[],"links":{"next":%q}}`, next)
			}))
			defer server.Close()
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			require.NoError(t, err)
			_, err = NewClient(server.URL+"/v1", "key", "issuer", key).ListIOSDevices(context.Background())
			require.Error(t, err)
		})
	}
}

// TestClientRejectsPartialLists propagates failures on a later page instead of hiding missing resources.
func TestClientRejectsPartialLists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") != "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"first"}],"links":{"next":"?cursor=second"}}`)
	}))
	defer server.Close()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	client := NewClient(server.URL+"/v1", "key", "issuer", key)
	devices, err := client.ListIOSDevices(context.Background())
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Nil(t, devices)
	certificates, err := client.ListDistributionCertificates(context.Background())
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Nil(t, certificates)
}

func pemPrivateKey(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func fakeClient(t *testing.T, server *appstoreconnecttest.Server) *Client {
	t.Helper()
	key, err := ParsePrivateKey(server.PrivateKeyPEM)
	require.NoError(t, err)
	return NewClient(server.BaseURL, appstoreconnecttest.KeyID, appstoreconnecttest.IssuerID, key)
}

// The fake server verifies the ES256 signature, kid, issuer, audience and lifetime of every token.
func TestClientSignsRequestsWithTheAPIKey(t *testing.T) {
	server := appstoreconnecttest.New(t)
	ctx := context.Background()
	client := fakeClient(t, server)
	require.NoError(t, client.VerifyAccess(ctx))

	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	for name, other := range map[string]*Client{
		"other private key": NewClient(server.BaseURL, appstoreconnecttest.KeyID, appstoreconnecttest.IssuerID, otherKey),
		"other key id":      NewClient(server.BaseURL, "ZZZ999ZZZZ", appstoreconnecttest.IssuerID, client.privateKey),
		"other issuer":      NewClient(server.BaseURL, appstoreconnecttest.KeyID, "00000000-0000-0000-0000-000000000000", client.privateKey),
	} {
		var apiErr *APIError
		require.ErrorAs(t, other.VerifyAccess(ctx), &apiErr, name)
		assert.Equal(t, http.StatusUnauthorized, apiErr.Status, name)
	}
}

func TestListIOSDevicesLeavesOutMacs(t *testing.T) {
	server := appstoreconnecttest.New(t)
	server.Devices = []appstoreconnecttest.Device{
		{ID: "IPHONE", Platform: "IOS", DeviceClass: "IPHONE", Status: "ENABLED"},
		{ID: "IPAD", Platform: "UNIVERSAL", DeviceClass: "IPAD", Status: "ENABLED"},
		{ID: "MAC", Platform: "MAC_OS", DeviceClass: "MAC", Status: "ENABLED"},
		{ID: "UNIVERSAL-MAC", Platform: "UNIVERSAL", DeviceClass: "MAC", Status: "ENABLED"},
		{ID: "DISABLED-IPHONE", Platform: "IOS", DeviceClass: "IPHONE", Status: "DISABLED"},
	}
	devices, err := fakeClient(t, server).ListIOSDevices(context.Background())
	require.NoError(t, err)
	ids := []string{}
	for _, device := range devices {
		ids = append(ids, device.ID)
	}
	assert.Equal(t, []string{"IPHONE", "IPAD", "DISABLED-IPHONE"}, ids)
}

func TestFindDeviceReturnsStatus(t *testing.T) {
	server := appstoreconnecttest.New(t)
	server.Devices = []appstoreconnecttest.Device{{ID: "DEVICE1", UDID: "UDID-1", Platform: "IOS", DeviceClass: "IPHONE", Status: "DISABLED"}}
	client := fakeClient(t, server)
	device, err := client.FindDevice(context.Background(), "UDID-1")
	require.NoError(t, err)
	require.NotNil(t, device)
	assert.Equal(t, "DEVICE1", device.ID)
	assert.Equal(t, "DISABLED", device.Status)
	missing, err := client.FindDevice(context.Background(), "UDID-2")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestClientErrors(t *testing.T) {
	server := appstoreconnecttest.New(t)
	client := fakeClient(t, server)
	ctx := context.Background()

	var apiErr *APIError
	server.Status = http.StatusForbidden
	require.ErrorAs(t, client.VerifyAccess(ctx), &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.Status)
	assert.Equal(t, "injected failure", apiErr.Detail)

	for _, status := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		server.Status = status
		err := client.VerifyAccess(ctx)
		assert.ErrorIs(t, err, ErrUnavailable, status)
		assert.False(t, errors.As(err, &apiErr), status)
	}

	unreachable := NewClient("http://127.0.0.1:1/v1", appstoreconnecttest.KeyID, appstoreconnecttest.IssuerID, client.privateKey)
	assert.ErrorIs(t, unreachable.VerifyAccess(ctx), ErrUnavailable)
}

func TestParsePrivateKey(t *testing.T) {
	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	_, err = ParsePrivateKey(pemPrivateKey(t, p256))
	require.NoError(t, err)

	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for name, text := range map[string]string{
		"P-384":             pemPrivateKey(t, p384),
		"RSA":               pemPrivateKey(t, rsaKey),
		"garbage":           "not a key",
		"certificate block": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("x")})),
	} {
		_, err := ParsePrivateKey(text)
		assert.Error(t, err, name)
	}
}

func TestBundleIDs(t *testing.T) {
	server := appstoreconnecttest.New(t)
	client := fakeClient(t, server)
	ctx := context.Background()

	missing, err := client.FindBundleID(ctx, "com.example.app")
	require.NoError(t, err)
	assert.Nil(t, missing)

	created, err := client.CreateBundleID(ctx, "com.example.app", "Example App")
	require.NoError(t, err)
	assert.Equal(t, "com.example.app", created.Identifier)

	// The identifier filter is a substring match at Apple; only the exact identifier counts.
	found, err := client.FindBundleID(ctx, "com.example.ap")
	require.NoError(t, err)
	assert.Nil(t, found)
	found, err = client.FindBundleID(ctx, "com.example.app")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)
}

func TestCertificateLifecycle(t *testing.T) {
	server := appstoreconnecttest.New(t)
	client := fakeClient(t, server)
	ctx := context.Background()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	require.NoError(t, err)
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))

	created, err := client.CreateDistributionCertificate(ctx, csrPEM)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(created.DER)
	require.NoError(t, err)
	assert.Equal(t, key.Public(), certificate.PublicKey)
	assert.Equal(t, []string{appstoreconnecttest.TeamID}, certificate.Subject.OrganizationalUnit)

	listed, err := client.ListDistributionCertificates(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, created.ID, listed[0].ID)

	server.CertificateLimitReached = true
	var apiErr *APIError
	_, err = client.CreateDistributionCertificate(ctx, csrPEM)
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.Status)

	require.NoError(t, client.RevokeCertificate(ctx, created.ID))
	listed, err = client.ListDistributionCertificates(ctx)
	require.NoError(t, err)
	assert.Empty(t, listed)
	require.ErrorAs(t, client.RevokeCertificate(ctx, created.ID), &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
}

func TestProfileLifecycle(t *testing.T) {
	server := appstoreconnecttest.New(t)
	client := fakeClient(t, server)
	ctx := context.Background()
	server.BundleIDs = []appstoreconnecttest.BundleID{{ID: "BUNDLE1", Identifier: "com.example.app", Name: "Example"}}
	server.Devices = []appstoreconnecttest.Device{{ID: "DEVICE1", UDID: "UDID-1", Platform: "IOS", Status: "ENABLED"}}
	certificateID := server.RegisterCertificate([]byte("certificate"))

	missing, err := client.FindProfile(ctx, "xprem com.example.app ad-hoc")
	require.NoError(t, err)
	assert.Nil(t, missing)

	_, err = client.CreateProfile(ctx, ProfileInput{Name: "xprem com.example.app ad-hoc", Type: ProfileTypeAdHoc, BundleID: "BUNDLE1", CertificateIDs: []string{certificateID}})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.Status)

	created, err := client.CreateProfile(ctx, ProfileInput{Name: "xprem com.example.app ad-hoc", Type: ProfileTypeAdHoc, BundleID: "BUNDLE1", CertificateIDs: []string{certificateID}, DeviceIDs: []string{"DEVICE1"}})
	require.NoError(t, err)
	assert.Equal(t, ProfileStateActive, created.State)
	assert.NotEmpty(t, created.Content)

	found, err := client.FindProfile(ctx, "xprem com.example.app ad-hoc")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, ProfileTypeAdHoc, found.Type)
	assert.Equal(t, created.Content, found.Content)

	certificateIDs, err := client.ProfileCertificateIDs(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{certificateID}, certificateIDs)
	deviceIDs, err := client.ProfileDeviceIDs(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"DEVICE1"}, deviceIDs)

	require.NoError(t, client.DeleteProfile(ctx, created.ID))
	found, err = client.FindProfile(ctx, "xprem com.example.app ad-hoc")
	require.NoError(t, err)
	assert.Nil(t, found)
}

// Apple's identifier filter matches substrings, so the exact bundle id can sit on a later page.
func TestFindBundleIDFollowsPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := map[string]any{"data": []map[string]any{{"id": "WIDGET", "attributes": map[string]string{"identifier": "com.example.app.widget"}}}}
		if r.URL.Query().Get("cursor") == "" {
			query := r.URL.Query()
			query.Set("cursor", "page-2")
			page["links"] = map[string]string{"next": "http://" + r.Host + r.URL.Path + "?" + query.Encode()}
		} else {
			page["data"] = []map[string]any{{"id": "APP", "attributes": map[string]string{"identifier": "com.example.app"}}}
		}
		assert.NoError(t, json.NewEncoder(w).Encode(page))
	}))
	defer server.Close()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	found, err := NewClient(server.URL+"/v1", "key", "issuer", key).FindBundleID(context.Background(), "com.example.app")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "APP", found.ID)
}
