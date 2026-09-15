package ios

import (
	"testing"
	"xprem/internal/ios/iostest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"howett.net/plist"
)

func TestRegistrationProfile(t *testing.T) {
	data, err := RegistrationProfile(RegistrationProfileInput{
		PayloadUUID: "0B8C6B3E-2F1A-4C55-9D2E-7A1B3C4D5E6F",
		AppName:     "Example",
		EnrollURL:   "https://ota.example.com/device-registrations/token/enroll",
		Challenge:   "challenge-1",
	})
	require.NoError(t, err)
	var profile struct {
		PayloadType         string `plist:"PayloadType"`
		PayloadVersion      int    `plist:"PayloadVersion"`
		PayloadIdentifier   string `plist:"PayloadIdentifier"`
		PayloadUUID         string `plist:"PayloadUUID"`
		PayloadDisplayName  string `plist:"PayloadDisplayName"`
		PayloadDescription  string `plist:"PayloadDescription"`
		PayloadOrganization string `plist:"PayloadOrganization"`
		PayloadContent      struct {
			URL              string   `plist:"URL"`
			DeviceAttributes []string `plist:"DeviceAttributes"`
			Challenge        string   `plist:"Challenge"`
		} `plist:"PayloadContent"`
	}
	format, err := plist.Unmarshal(data, &profile)
	require.NoError(t, err)
	assert.Equal(t, plist.XMLFormat, format)
	assert.Equal(t, "Profile Service", profile.PayloadType)
	assert.Equal(t, 1, profile.PayloadVersion)
	assert.Equal(t, "dev.xprem.device-registration.0B8C6B3E-2F1A-4C55-9D2E-7A1B3C4D5E6F", profile.PayloadIdentifier)
	assert.Equal(t, "0B8C6B3E-2F1A-4C55-9D2E-7A1B3C4D5E6F", profile.PayloadUUID)
	assert.Equal(t, "Register this iPhone for Example", profile.PayloadDisplayName)
	assert.NotEmpty(t, profile.PayloadDescription)
	assert.Equal(t, "xprem", profile.PayloadOrganization)
	assert.Equal(t, "https://ota.example.com/device-registrations/token/enroll", profile.PayloadContent.URL)
	assert.Equal(t, []string{"UDID", "PRODUCT", "VERSION", "DEVICE_NAME", "SERIAL"}, profile.PayloadContent.DeviceAttributes)
	assert.Equal(t, "challenge-1", profile.PayloadContent.Challenge)
}

func deviceResponse(t *testing.T, attributes map[string]string) []byte {
	t.Helper()
	content, err := plist.Marshal(attributes, plist.XMLFormat)
	require.NoError(t, err)
	return iostest.SignedData(content, true)
}

func TestParseDeviceResponse(t *testing.T) {
	attributes, err := ParseDeviceResponse(deviceResponse(t, map[string]string{
		"UDID": "00008110-000A1B2C3D4E801E", "PRODUCT": "iPhone15,2", "VERSION": "22A3354",
		"DEVICE_NAME": "Jane's iPhone", "SERIAL": "F2LXXXXXXX", "CHALLENGE": "challenge-1",
	}), "challenge-1")
	require.NoError(t, err)
	assert.Equal(t, &DeviceAttributes{
		UDID: "00008110-000A1B2C3D4E801E", Product: "iPhone15,2", Version: "22A3354",
		DeviceName: "Jane's iPhone", Serial: "F2LXXXXXXX", Challenge: "challenge-1",
	}, attributes)

	withoutOptional, err := ParseDeviceResponse(deviceResponse(t, map[string]string{
		"UDID": "00008110-000A1B2C3D4E801E", "PRODUCT": "iPhone15,2", "VERSION": "22A3354", "CHALLENGE": "challenge-1",
	}), "challenge-1")
	require.NoError(t, err)
	assert.Empty(t, withoutOptional.DeviceName)
	assert.Empty(t, withoutOptional.Serial)

	for name, data := range map[string][]byte{
		"wrong challenge": deviceResponse(t, map[string]string{"UDID": "UDID-1", "CHALLENGE": "other"}),
		"no challenge":    deviceResponse(t, map[string]string{"UDID": "UDID-1"}),
		"no udid":         deviceResponse(t, map[string]string{"CHALLENGE": "challenge-1"}),
		"not cms":         []byte("<?xml version=\"1.0\"?><plist><dict/></plist>"),
		"not a plist":     iostest.SignedData([]byte("garbage"), false),
	} {
		_, err := ParseDeviceResponse(data, "challenge-1")
		assert.Error(t, err, name)
	}
}
