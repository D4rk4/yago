package crawlorder

import (
	"crypto/ed25519"
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

func ephemeralTLSTransportCredentials(
	t *testing.T,
) (credentials.TransportCredentials, credentials.TransportCredentials) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ephemeral TLS key: %v", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		t.Fatalf("generate ephemeral TLS certificate serial: %v", err)
	}
	serial.Add(serial, big.NewInt(1))
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(5 * time.Minute),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		BasicConstraintsValid: true,
	}
	certificateData, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		publicKey,
		privateKey,
	)
	if err != nil {
		t.Fatalf("create ephemeral TLS certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(certificateData)
	if err != nil {
		t.Fatalf("parse ephemeral TLS certificate: %v", err)
	}
	serverCertificate := tls.Certificate{
		Certificate: [][]byte{certificateData},
		PrivateKey:  privateKey,
		Leaf:        certificate,
	}
	trustedCertificates := x509.NewCertPool()
	trustedCertificates.AddCert(certificate)

	return credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{serverCertificate},
			MinVersion:   tls.VersionTLS13,
		}), credentials.NewTLS(&tls.Config{
			RootCAs:    trustedCertificates,
			ServerName: "localhost",
			MinVersion: tls.VersionTLS13,
		})
}
