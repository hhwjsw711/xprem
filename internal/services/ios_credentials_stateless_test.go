package services

import (
	"context"
	"testing"
	"xprem/internal/store"
	"xprem/internal/types"

	"github.com/stretchr/testify/assert"
)

func TestIosCredentialsUnsupportedInStatelessMode(t *testing.T) {
	service := NewIosCredentialsService(nil, nil)
	ctx := context.Background()
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	_, err := service.GetIosCredentialsMetadata(ctx, testAppId, testIdentifierId)
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	assert.ErrorIs(t, service.UpdateIosSigningSetting(ctx, testAppId, testIdentifierId, IosSigningSettingInput{Mode: types.IosSigningAutomatic}), store.ErrNotSupportedInStatelessMode)
	assert.ErrorIs(t, service.SaveAppStoreConnectApiKey(ctx, testAppId, AppStoreConnectApiKeyInput{}), store.ErrNotSupportedInStatelessMode)
	_, err = service.GetAppStoreConnectApiKeyMetadata(ctx, testAppId)
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	assert.ErrorIs(t, service.DeleteAppStoreConnectApiKey(ctx, testAppId), store.ErrNotSupportedInStatelessMode)
	_, err = service.ListIosSigningCertificates(ctx, testAppId, testIdentifierId)
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	_, err = service.ImportIosCertificate(ctx, testAppId, testIdentifierId, IosCertificateImportInput{})
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	_, _, err = service.CreateIosDeviceInvitation(ctx, testAppId, "", 0)
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	_, err = service.ListIosDeviceInvitations(ctx, testAppId)
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	assert.ErrorIs(t, service.RevokeIosDeviceInvitation(ctx, testAppId, testIdentifierId), store.ErrNotSupportedInStatelessMode)
	_, err = service.ListAppleDevices(ctx, testAppId)
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	assert.ErrorIs(t, service.SetAppleDeviceEnabled(ctx, testAppId, "DEVICE1", true), store.ErrNotSupportedInStatelessMode)
	_, err = service.GetPublicIosDeviceInvitation(ctx, token)
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	_, err = service.GetPublicIosDeviceRegistration(ctx, token, testIdentifierId)
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	_, err = service.IosDeviceRegistrationProfile(ctx, token, "https://ota.example.com/enroll")
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
	_, err = service.EnrollIosDevice(ctx, token, []byte("device response"))
	assert.ErrorIs(t, err, store.ErrNotSupportedInStatelessMode)
}
