package types

// IosCertificateType is the kind of Apple signing certificate.
type IosCertificateType string

const (
	IosCertificateDistribution IosCertificateType = "distribution"
	IosCertificateDevelopment  IosCertificateType = "development"
)

// IosSigningMode says whether xprem picks the signing certificate or uses a selected pool certificate.
type IosSigningMode string

const (
	IosSigningAutomatic   IosSigningMode = "automatic"
	IosSigningCertificate IosSigningMode = "certificate"
)

// IosDeviceRegistrationStatus is the outcome of registering an iPhone at Apple through a link.
type IosDeviceRegistrationStatus string

const (
	IosDeviceRegistered         IosDeviceRegistrationStatus = "registered"
	IosDeviceRegistrationFailed IosDeviceRegistrationStatus = "failed"
)
