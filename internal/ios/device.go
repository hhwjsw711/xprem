package ios

import (
	"crypto/subtle"
	"errors"

	"howett.net/plist"
)

// RegistrationProfileInput describes the Profile Service configuration profile of one registration link.
type RegistrationProfileInput struct {
	PayloadUUID string
	AppName     string
	EnrollURL   string
	Challenge   string
}

// RegistrationProfile renders an unsigned .mobileconfig that makes iOS post its device attributes to EnrollURL.
func RegistrationProfile(input RegistrationProfileInput) ([]byte, error) {
	document := map[string]any{
		"PayloadType":         "Profile Service",
		"PayloadVersion":      1,
		"PayloadIdentifier":   "dev.xprem.device-registration." + input.PayloadUUID,
		"PayloadUUID":         input.PayloadUUID,
		"PayloadDisplayName":  "Register this iPhone for " + input.AppName,
		"PayloadDescription":  "Sends this iPhone's identifier and model so it can be added to the Apple Developer account of " + input.AppName + " and install its Ad Hoc builds.",
		"PayloadOrganization": "xprem",
		"PayloadContent": map[string]any{
			"URL":              input.EnrollURL,
			"DeviceAttributes": []string{"UDID", "PRODUCT", "VERSION", "DEVICE_NAME", "SERIAL"},
			"Challenge":        input.Challenge,
		},
	}
	return plist.MarshalIndent(document, plist.XMLFormat, "\t")
}

// DeviceAttributes is what an iPhone sends back after installing a registration profile.
type DeviceAttributes struct {
	UDID       string `plist:"UDID"`
	Product    string `plist:"PRODUCT"`
	Version    string `plist:"VERSION"`
	DeviceName string `plist:"DEVICE_NAME"`
	Serial     string `plist:"SERIAL"`
	Challenge  string `plist:"CHALLENGE"`
}

var errInvalidDeviceResponse = errors.New("invalid device response")

// ParseDeviceResponse reads the CMS-wrapped attributes posted by iOS and checks the challenge.
// The CMS signature is not verified.
func ParseDeviceResponse(data []byte, challenge string) (*DeviceAttributes, error) {
	content, err := signedDataContent(data)
	if err != nil {
		return nil, errInvalidDeviceResponse
	}
	var attributes DeviceAttributes
	if _, err := plist.Unmarshal(content, &attributes); err != nil {
		return nil, errInvalidDeviceResponse
	}
	if attributes.UDID == "" || subtle.ConstantTimeCompare([]byte(attributes.Challenge), []byte(challenge)) != 1 {
		return nil, errInvalidDeviceResponse
	}
	return &attributes, nil
}
