// Package appstoreconnect is a minimal App Store Connect API client for
// signing certificates, bundle ids, provisioning profiles and devices.
package appstoreconnect

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	DefaultBaseURL   = "https://api.appstoreconnect.apple.com/v1"
	tokenLifetime    = 10 * time.Minute
	maxResponseBytes = 10 << 20
)

// ErrUnavailable reports that Apple could not be reached or failed on its side.
var ErrUnavailable = errors.New("App Store Connect is unavailable")

// APIError is a client error answered by App Store Connect.
type APIError struct {
	Status int
	Detail string
}

// Error includes the Apple status and detail for server-side diagnostics.
func (e *APIError) Error() string {
	return fmt.Sprintf("App Store Connect answered %d: %s", e.Status, e.Detail)
}

// Client calls App Store Connect with a team API key.
type Client struct {
	baseURL    string
	httpClient *http.Client
	keyID      string
	issuerID   string
	privateKey *ecdsa.PrivateKey
}

// Certificate is a signing certificate registered in the team.
type Certificate struct {
	ID  string
	DER []byte
}

// BundleID is an app identifier registered in the team.
type BundleID struct {
	ID         string
	Identifier string
}

// Profile types and states as Apple names them.
const (
	ProfileTypeAppStore = "IOS_APP_STORE"
	ProfileTypeAdHoc    = "IOS_APP_ADHOC"
	ProfileStateActive  = "ACTIVE"
)

// Profile is a provisioning profile of the team; Content is the .mobileprovision file.
type Profile struct {
	ID        string
	Name      string
	Type      string
	State     string
	Content   []byte
	ExpiresAt string
}

// ProfileInput describes a provisioning profile to create; the ids are Apple resource ids.
type ProfileInput struct {
	Name           string
	Type           string
	BundleID       string
	CertificateIDs []string
	DeviceIDs      []string
}

// Device is a device registered in the team; AddedDate is Apple's raw timestamp.
type Device struct {
	ID          string
	Name        string
	UDID        string
	Model       string
	DeviceClass string
	Status      string
	AddedDate   string
}

// NewClient creates a team API client with a one-minute request timeout.
func NewClient(baseURL, keyID, issuerID string, privateKey *ecdsa.PrivateKey) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: time.Minute},
		keyID:      keyID,
		issuerID:   issuerID,
		privateKey: privateKey,
	}
}

// ParsePrivateKey reads the PEM PKCS#8 P-256 key of a .p8 file.
func ParsePrivateKey(pemText string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("not a PEM private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok || ecKey.Curve != elliptic.P256() {
		return nil, errors.New("not an EC P-256 private key")
	}
	return ecKey, nil
}

type resource[T any] struct {
	ID         string `json:"id"`
	Attributes T      `json:"attributes"`
}

type listDocument[T any] struct {
	Data  []resource[T] `json:"data"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

type document[T any] struct {
	Data resource[T] `json:"data"`
}

type certificateAttributes struct {
	CertificateContent []byte `json:"certificateContent"`
}

type bundleIDAttributes struct {
	Identifier string `json:"identifier"`
}

type profileAttributes struct {
	Name           string `json:"name"`
	ProfileType    string `json:"profileType"`
	ProfileState   string `json:"profileState"`
	ProfileContent []byte `json:"profileContent"`
	ExpirationDate string `json:"expirationDate"`
}

// profile combines the Apple resource ID with its profile attributes.
func (a profileAttributes) profile(id string) Profile {
	return Profile{ID: id, Name: a.Name, Type: a.ProfileType, State: a.ProfileState, Content: a.ProfileContent, ExpiresAt: a.ExpirationDate}
}

type resourceRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func resourceRefs(kind string, ids []string) []resourceRef {
	refs := make([]resourceRef, len(ids))
	for i, id := range ids {
		refs[i] = resourceRef{Type: kind, ID: id}
	}
	return refs
}

type deviceAttributes struct {
	Name        string `json:"name"`
	UDID        string `json:"udid"`
	Model       string `json:"model"`
	Platform    string `json:"platform"`
	DeviceClass string `json:"deviceClass"`
	Status      string `json:"status"`
	AddedDate   string `json:"addedDate"`
}

// device combines the Apple resource ID with its device attributes.
func (a deviceAttributes) device(id string) Device {
	return Device{ID: id, Name: a.Name, UDID: a.UDID, Model: a.Model, DeviceClass: a.DeviceClass, Status: a.Status, AddedDate: a.AddedDate}
}

// VerifyAccess makes the cheapest authenticated call.
func (c *Client) VerifyAccess(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/bundleIds?limit=1", nil, nil)
}

// ListDistributionCertificates downloads every page of the team's distribution certificates.
func (c *Client) ListDistributionCertificates(ctx context.Context) ([]Certificate, error) {
	query := url.Values{"filter[certificateType]": {"DISTRIBUTION,IOS_DISTRIBUTION"}, "limit": {"200"}}
	resources, err := listResources[certificateAttributes](ctx, c, "/certificates?"+query.Encode())
	if err != nil {
		return nil, err
	}
	certificates := make([]Certificate, len(resources))
	for i, certificate := range resources {
		certificates[i] = Certificate{ID: certificate.ID, DER: certificate.Attributes.CertificateContent}
	}
	return certificates, nil
}

// CreateDistributionCertificate has Apple sign a PEM certificate request as an iOS distribution certificate.
func (c *Client) CreateDistributionCertificate(ctx context.Context, csrPEM string) (Certificate, error) {
	body := map[string]any{"data": map[string]any{
		"type":       "certificates",
		"attributes": map[string]string{"csrContent": csrPEM, "certificateType": "IOS_DISTRIBUTION"},
	}}
	var created document[certificateAttributes]
	if err := c.do(ctx, http.MethodPost, "/certificates", body, &created); err != nil {
		return Certificate{}, err
	}
	return Certificate{ID: created.Data.ID, DER: created.Data.Attributes.CertificateContent}, nil
}

// RevokeCertificate revokes a certificate of the team.
func (c *Client) RevokeCertificate(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/certificates/"+url.PathEscape(id), nil, nil)
}

// FindBundleID returns the team's bundle id with this identifier, or nil when it is not registered.
func (c *Client) FindBundleID(ctx context.Context, identifier string) (*BundleID, error) {
	query := url.Values{"filter[identifier]": {identifier}, "limit": {"200"}}
	bundleIDs, err := listResources[bundleIDAttributes](ctx, c, "/bundleIds?"+query.Encode())
	if err != nil {
		return nil, err
	}
	for _, bundleID := range bundleIDs {
		if bundleID.Attributes.Identifier == identifier {
			return &BundleID{ID: bundleID.ID, Identifier: identifier}, nil
		}
	}
	return nil, nil
}

// CreateBundleID registers an iOS bundle id in the team.
func (c *Client) CreateBundleID(ctx context.Context, identifier string, name string) (BundleID, error) {
	body := map[string]any{"data": map[string]any{
		"type":       "bundleIds",
		"attributes": map[string]string{"identifier": identifier, "name": name, "platform": "IOS"},
	}}
	var created document[bundleIDAttributes]
	if err := c.do(ctx, http.MethodPost, "/bundleIds", body, &created); err != nil {
		return BundleID{}, err
	}
	return BundleID{ID: created.Data.ID, Identifier: created.Data.Attributes.Identifier}, nil
}

// FindProfile returns the team's provisioning profile with this name, or nil when there is none.
func (c *Client) FindProfile(ctx context.Context, name string) (*Profile, error) {
	query := url.Values{"filter[name]": {name}, "limit": {"200"}}
	profiles, err := listResources[profileAttributes](ctx, c, "/profiles?"+query.Encode())
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if profile.Attributes.Name == name {
			found := profile.Attributes.profile(profile.ID)
			return &found, nil
		}
	}
	return nil, nil
}

// ProfileCertificateIDs lists the Apple ids of the certificates a profile includes.
func (c *Client) ProfileCertificateIDs(ctx context.Context, profileID string) ([]string, error) {
	return c.relatedIDs(ctx, "/profiles/"+url.PathEscape(profileID)+"/relationships/certificates")
}

// ProfileDeviceIDs lists the Apple ids of the devices a profile includes.
func (c *Client) ProfileDeviceIDs(ctx context.Context, profileID string) ([]string, error) {
	return c.relatedIDs(ctx, "/profiles/"+url.PathEscape(profileID)+"/relationships/devices")
}

func (c *Client) relatedIDs(ctx context.Context, path string) ([]string, error) {
	resources, err := listResources[struct{}](ctx, c, path+"?limit=200")
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(resources))
	for i, resource := range resources {
		ids[i] = resource.ID
	}
	return ids, nil
}

// CreateProfile creates a provisioning profile and returns it with its content.
func (c *Client) CreateProfile(ctx context.Context, input ProfileInput) (Profile, error) {
	body := map[string]any{"data": map[string]any{
		"type":       "profiles",
		"attributes": map[string]string{"name": input.Name, "profileType": input.Type},
		"relationships": map[string]any{
			"bundleId":     map[string]any{"data": resourceRef{Type: "bundleIds", ID: input.BundleID}},
			"certificates": map[string]any{"data": resourceRefs("certificates", input.CertificateIDs)},
			"devices":      map[string]any{"data": resourceRefs("devices", input.DeviceIDs)},
		},
	}}
	var created document[profileAttributes]
	if err := c.do(ctx, http.MethodPost, "/profiles", body, &created); err != nil {
		return Profile{}, err
	}
	return created.Data.Attributes.profile(created.Data.ID), nil
}

// DeleteProfile deletes a provisioning profile of the team.
func (c *Client) DeleteProfile(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/profiles/"+url.PathEscape(id), nil, nil)
}

// ListIOSDevices returns the team's devices that are not Macs, enabled or not.
func (c *Client) ListIOSDevices(ctx context.Context) ([]Device, error) {
	query := url.Values{"limit": {"200"}}
	resources, err := listResources[deviceAttributes](ctx, c, "/devices?"+query.Encode())
	if err != nil {
		return nil, err
	}
	devices := []Device{}
	for _, device := range resources {
		if device.Attributes.Platform != "MAC_OS" && device.Attributes.DeviceClass != "MAC" {
			devices = append(devices, device.Attributes.device(device.ID))
		}
	}
	return devices, nil
}

// listResources follows Apple's next links and fails instead of returning a partial list.
func listResources[T any](ctx context.Context, client *Client, path string) ([]resource[T], error) {
	pageURL, err := url.Parse(client.baseURL + path)
	if err != nil {
		return nil, err
	}
	var resources []resource[T]
	seen := map[string]bool{}
	for {
		if seen[pageURL.String()] {
			return nil, fmt.Errorf("%w: repeated pagination link", ErrUnavailable)
		}
		seen[pageURL.String()] = true
		var page listDocument[T]
		if err := client.do(ctx, http.MethodGet, pageURL.String(), nil, &page); err != nil {
			return nil, err
		}
		resources = append(resources, page.Data...)
		if page.Links.Next == "" {
			return resources, nil
		}
		next, err := url.Parse(page.Links.Next)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid pagination link", ErrUnavailable)
		}
		pageURL = pageURL.ResolveReference(next)
	}
}

// FindDevice returns the team's device with this UDID, or nil when it is not registered.
func (c *Client) FindDevice(ctx context.Context, udid string) (*Device, error) {
	query := url.Values{"filter[udid]": {udid}, "limit": {"200"}}
	var list listDocument[deviceAttributes]
	if err := c.do(ctx, http.MethodGet, "/devices?"+query.Encode(), nil, &list); err != nil {
		return nil, err
	}
	for _, device := range list.Data {
		if strings.EqualFold(device.Attributes.UDID, udid) {
			found := device.Attributes.device(device.ID)
			return &found, nil
		}
	}
	return nil, nil
}

// RegisterIOSDevice adds an iOS device to the team and returns its Apple id.
func (c *Client) RegisterIOSDevice(ctx context.Context, name, udid string) (string, error) {
	body := map[string]any{"data": map[string]any{
		"type":       "devices",
		"attributes": map[string]string{"name": name, "platform": "IOS", "udid": udid},
	}}
	var created document[deviceAttributes]
	if err := c.do(ctx, http.MethodPost, "/devices", body, &created); err != nil {
		return "", err
	}
	return created.Data.ID, nil
}

// UpdateDeviceStatus enables or disables a device of the team and returns it.
func (c *Client) UpdateDeviceStatus(ctx context.Context, id string, status string) (Device, error) {
	body := map[string]any{"data": map[string]any{
		"type":       "devices",
		"id":         id,
		"attributes": map[string]string{"status": status},
	}}
	var updated document[deviceAttributes]
	if err := c.do(ctx, http.MethodPatch, "/devices/"+url.PathEscape(id), body, &updated); err != nil {
		return Device{}, err
	}
	return updated.Data.Attributes.device(updated.Data.ID), nil
}

// token signs a short-lived ES256 bearer token for App Store Connect.
func (c *Client) token() (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": c.issuerID,
		"iat": now.Unix(),
		"exp": now.Add(tokenLifetime).Unix(),
		"aud": "appstoreconnect-v1",
	})
	token.Header["kid"] = c.keyID
	return token.SignedString(c.privateKey)
}

// do authenticates one request within the configured API and decodes its response.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return err
	}
	requestURL, err := url.Parse(path)
	if err != nil {
		return err
	}
	if !requestURL.IsAbs() {
		requestURL, err = url.Parse(c.baseURL + path)
		if err != nil {
			return err
		}
	}
	// A next link must never send the team's bearer token to another origin.
	if requestURL.Scheme != base.Scheme || requestURL.Host != base.Host || requestURL.User != nil ||
		!strings.HasPrefix(requestURL.Path, strings.TrimRight(base.Path, "/")+"/") {
		return fmt.Errorf("%w: pagination link outside the API", ErrUnavailable)
	}
	token, err := c.token()
	if err != nil {
		return fmt.Errorf("sign app store connect token: %w", err)
	}
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), payload)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}
	if response.StatusCode >= 400 {
		return apiError(response.StatusCode, data)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: unexpected response to %s %s", ErrUnavailable, method, request.URL.Path)
	}
	return nil
}

// apiError extracts the first Apple error detail, falling back to the HTTP status.
func apiError(status int, data []byte) *APIError {
	var answer struct {
		Errors []struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	apiErr := &APIError{Status: status, Detail: http.StatusText(status)}
	if json.Unmarshal(data, &answer) == nil && len(answer.Errors) > 0 {
		first := answer.Errors[0]
		if first.Detail != "" {
			apiErr.Detail = first.Detail
		} else if first.Title != "" {
			apiErr.Detail = first.Title
		}
	}
	return apiErr
}
