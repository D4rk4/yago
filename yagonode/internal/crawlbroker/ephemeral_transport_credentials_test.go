package crawlbroker

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
)

const ephemeralTransportCertificateLifetime = 10 * time.Minute

func newEphemeralLoopbackTLSCredentials(
	t *testing.T,
) (credentials.TransportCredentials, credentials.TransportCredentials) {
	t.Helper()
	validFrom := time.Now().Add(-time.Minute)
	validUntil := validFrom.Add(ephemeralTransportCertificateLifetime)
	authorityCertificate, authorityKey := newEphemeralCertificateAuthority(
		t,
		validFrom,
		validUntil,
	)
	serverCertificate := newEphemeralLoopbackCertificate(
		t,
		authorityCertificate,
		authorityKey,
		validFrom,
		validUntil,
	)
	authorities := x509.NewCertPool()
	authorities.AddCert(authorityCertificate)

	serverCredentials := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverCertificate},
		MinVersion:   tls.VersionTLS13,
	})
	clientCredentials := credentials.NewTLS(&tls.Config{
		RootCAs:    authorities,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS13,
	})

	return serverCredentials, clientCredentials
}

func newEphemeralCertificateAuthority(
	t *testing.T,
	validFrom time.Time,
	validUntil time.Time,
) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ephemeral transport authority key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          newEphemeralCertificateSerial(t),
		Subject:               pkix.Name{CommonName: "yago crawlbroker test authority"},
		NotBefore:             validFrom,
		NotAfter:              validUntil,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certificateDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		t.Fatalf("create ephemeral transport authority: %v", err)
	}
	certificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		t.Fatalf("parse ephemeral transport authority: %v", err)
	}

	return certificate, privateKey
}

func newEphemeralLoopbackCertificate(
	t *testing.T,
	authorityCertificate *x509.Certificate,
	authorityKey *ecdsa.PrivateKey,
	validFrom time.Time,
	validUntil time.Time,
) tls.Certificate {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ephemeral loopback key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: newEphemeralCertificateSerial(t),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    validFrom,
		NotAfter:     validUntil,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	certificateDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		authorityCertificate,
		&privateKey.PublicKey,
		authorityKey,
	)
	if err != nil {
		t.Fatalf("create ephemeral loopback certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		t.Fatalf("parse ephemeral loopback certificate: %v", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certificateDER, authorityCertificate.Raw},
		PrivateKey:  privateKey,
		Leaf:        certificate,
	}
}

func newEphemeralCertificateSerial(t *testing.T) *big.Int {
	t.Helper()
	upperBound := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, upperBound)
	if err != nil {
		t.Fatalf("generate ephemeral certificate serial: %v", err)
	}

	return serialNumber.Add(serialNumber, big.NewInt(1))
}
