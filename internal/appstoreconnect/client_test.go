package appstoreconnect

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"testing"
	"xprem/internal/appstoreconnect/appstoreconnecttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
