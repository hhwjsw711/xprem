// Package iostest builds in-memory Apple signing certificates and CMS envelopes for tests.
package iostest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// Identity is a signing key with its self-signed certificate.
type Identity struct {
	Key         *ecdsa.PrivateKey
	Certificate *x509.Certificate
}

// NewIdentity creates a certificate whose subject has commonName and, when
// teamID is not empty, teamID as its organizational unit.
func NewIdentity(commonName, teamID string, notAfter time.Time) Identity {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		panic(err)
	}
	subject := pkix.Name{CommonName: commonName}
	if teamID != "" {
		subject.OrganizationalUnit = []string{teamID}
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      subject,
		NotBefore:    notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	return Identity{Key: key, Certificate: certificate}
}

// P12 encodes the identity as a PKCS#12 file.
func (i Identity) P12(password string) []byte {
	data, err := pkcs12.Modern.Encode(i.Key, i.Certificate, nil, password)
	if err != nil {
		panic(err)
	}
	return data
}

// SignedData wraps content in an unsigned CMS SignedData envelope. With
// indefinite, every constructed value uses an indefinite length and the
// content is split into several OCTET STRING segments.
func SignedData(content []byte, indefinite bool) []byte {
	encode := definiteTLV
	if indefinite {
		encode = indefiniteTLV
	}
	encapsulatedContent := definiteTLV(0x04, content)
	if indefinite {
		var segments []byte
		for start := 0; start < len(content); start += 1000 {
			segments = append(segments, definiteTLV(0x04, content[start:min(start+1000, len(content))])...)
		}
		encapsulatedContent = indefiniteTLV(0x24, segments)
	}
	signedData := concat(
		definiteTLV(0x02, []byte{1}),
		definiteTLV(0x31, nil),
		encode(0x30, concat(oid(1, 2, 840, 113549, 1, 7, 1), encode(0xa0, encapsulatedContent))),
		definiteTLV(0x31, nil),
	)
	return encode(0x30, concat(oid(1, 2, 840, 113549, 1, 7, 2), encode(0xa0, encode(0x30, signedData))))
}

func oid(components ...int) []byte {
	data, err := asn1.Marshal(asn1.ObjectIdentifier(components))
	if err != nil {
		panic(err)
	}
	return data
}

func definiteTLV(tag byte, content []byte) []byte {
	length := len(content)
	if length < 0x80 {
		return concat([]byte{tag, byte(length)}, content)
	}
	var lengthBytes []byte
	for ; length > 0; length >>= 8 {
		lengthBytes = append([]byte{byte(length)}, lengthBytes...)
	}
	return concat([]byte{tag, 0x80 | byte(len(lengthBytes))}, lengthBytes, content)
}

func indefiniteTLV(tag byte, content []byte) []byte {
	return concat([]byte{tag, 0x80}, content, []byte{0, 0})
}

func concat(parts ...[]byte) []byte {
	var joined []byte
	for _, part := range parts {
		joined = append(joined, part...)
	}
	return joined
}
