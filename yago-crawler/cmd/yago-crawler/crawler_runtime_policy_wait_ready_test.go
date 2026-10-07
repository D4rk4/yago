package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync/atomic"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type runtimePolicyReadinessServer struct {
	crawlrpc.UnimplementedCrawlExchangeServer
	policy *crawlrpc.CrawlerRuntimePolicy
}

func (s runtimePolicyReadinessServer) ReadRuntimePolicy(
	context.Context,
	*crawlrpc.CrawlerRuntimePolicyRequest,
) (*crawlrpc.CrawlerRuntimePolicy, error) {
	return s.policy, nil
}

type runtimePolicyReadinessDialer struct {
	listener      *bufconn.Listener
	firstAttempt  chan struct{}
	firstReleased chan struct{}
	firstReturned chan struct{}
	secondAttempt chan struct{}
	ready         chan struct{}
	attempts      atomic.Int32
}

func (d *runtimePolicyReadinessDialer) DialContext(
	ctx context.Context,
	_ string,
) (net.Conn, error) {
	if d.attempts.Add(1) == 1 {
		close(d.firstAttempt)
		select {
		case <-d.firstReleased:
			close(d.firstReturned)
			return nil, errors.New("connection refused")
		case <-ctx.Done():
			close(d.firstReturned)
			return nil, fmt.Errorf("readiness dial context: %w", ctx.Err())
		}
	}
	select {
	case <-d.secondAttempt:
	default:
		close(d.secondAttempt)
	}
	select {
	case <-d.ready:
		return d.listener.Dial()
	case <-ctx.Done():
		return nil, fmt.Errorf("readiness dial context: %w", ctx.Err())
	}
}

func TestReadCrawlerRuntimePolicyWaitsForTransientReadiness(t *testing.T) {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.MaximumDepth = 7
	message, err := yagocrawlcontract.CrawlerRuntimePolicyToProto(policy)
	if err != nil {
		t.Fatalf("encode runtime policy: %v", err)
	}
	listener, clientCredentials := startRuntimePolicyReadinessServer(t, message)
	dialer := newRuntimePolicyReadinessDialer(listener)
	installRuntimePolicyReadinessDialer(t, dialer, clientCredentials)
	config := minimalServiceConfig(t)
	config.NodeRPCAddr = "passthrough:///runtime-policy"
	config.Crawl.ConnectTimeout = 2 * time.Second
	result := make(chan error, 1)
	go func() {
		resolved, err := readCrawlerRuntimePolicy(t.Context(), config)
		if err == nil && resolved.Crawl.MaxDepth != 7 {
			err = errors.New("runtime policy response was not applied")
		}
		result <- err
	}()
	awaitRuntimePolicySignal(t, dialer.firstAttempt, "first readiness dial did not start")
	close(dialer.firstReleased)
	awaitRuntimePolicySignal(t, dialer.firstReturned, "first readiness dial did not fail")
	awaitRuntimePolicySignal(t, dialer.secondAttempt, "runtime policy did not retry its connection")
	close(dialer.ready)
	if err := receiveRuntimePolicyRead(t, result); err != nil {
		t.Fatalf("runtime policy read after readiness: %v", err)
	}
}

func TestReadCrawlerRuntimePolicyWaitRespectsConnectDeadline(t *testing.T) {
	listener, clientCredentials := startRuntimePolicyReadinessServer(t, nil)
	dialer := newRuntimePolicyReadinessDialer(listener)
	installRuntimePolicyReadinessDialer(t, dialer, clientCredentials)
	config := minimalServiceConfig(t)
	config.NodeRPCAddr = "passthrough:///runtime-policy"
	config.Crawl.ConnectTimeout = 100 * time.Millisecond
	result := make(chan error, 1)
	go func() {
		_, err := readCrawlerRuntimePolicy(t.Context(), config)
		result <- err
	}()
	awaitRuntimePolicySignal(t, dialer.firstAttempt, "deadline readiness dial did not start")
	close(dialer.firstReleased)
	awaitRuntimePolicySignal(t, dialer.secondAttempt, "deadline readiness dial did not retry")
	err := receiveRuntimePolicyRead(t, result)
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("runtime policy read after deadline = %v, want deadline exceeded", err)
	}
}

func TestReadCrawlerRuntimePolicyWaitRespectsCallerCancellation(t *testing.T) {
	listener, clientCredentials := startRuntimePolicyReadinessServer(t, nil)
	dialer := newRuntimePolicyReadinessDialer(listener)
	installRuntimePolicyReadinessDialer(t, dialer, clientCredentials)
	config := minimalServiceConfig(t)
	config.NodeRPCAddr = "passthrough:///runtime-policy"
	result := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_, err := readCrawlerRuntimePolicy(ctx, config)
		result <- err
	}()
	awaitRuntimePolicySignal(t, dialer.firstAttempt, "cancellation readiness dial did not start")
	close(dialer.firstReleased)
	awaitRuntimePolicySignal(t, dialer.secondAttempt, "cancellation readiness dial did not retry")
	cancel()
	err := receiveRuntimePolicyRead(t, result)
	if status.Code(err) != codes.Canceled {
		t.Fatalf("runtime policy read after caller cancellation = %v, want canceled", err)
	}
}

func startRuntimePolicyReadinessServer(
	t *testing.T,
	policy *crawlrpc.CrawlerRuntimePolicy,
) (*bufconn.Listener, credentials.TransportCredentials) {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	serverCredentials, clientCredentials := runtimePolicyReadinessTransportCredentials(t)
	server := grpc.NewServer(grpc.Creds(serverCredentials))
	crawlrpc.RegisterCrawlExchangeServer(server, runtimePolicyReadinessServer{policy: policy})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	t.Cleanup(func() { _ = listener.Close() })

	return listener, clientCredentials
}

func runtimePolicyReadinessTransportCredentials(
	t *testing.T,
) (credentials.TransportCredentials, credentials.TransportCredentials) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate readiness TLS key: %v", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		t.Fatalf("generate readiness TLS serial: %v", err)
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
		t.Fatalf("create readiness TLS certificate: %v", err)
	}
	certificate, err := x509.ParseCertificate(certificateData)
	if err != nil {
		t.Fatalf("parse readiness TLS certificate: %v", err)
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

func newRuntimePolicyReadinessDialer(listener *bufconn.Listener) *runtimePolicyReadinessDialer {
	return &runtimePolicyReadinessDialer{
		listener:      listener,
		firstAttempt:  make(chan struct{}),
		firstReleased: make(chan struct{}),
		firstReturned: make(chan struct{}),
		secondAttempt: make(chan struct{}),
		ready:         make(chan struct{}),
	}
}

func installRuntimePolicyReadinessDialer(
	t *testing.T,
	dialer *runtimePolicyReadinessDialer,
	clientCredentials credentials.TransportCredentials,
) {
	t.Helper()
	previous := newCrawlerExchange
	newCrawlerExchange = func(address string) (crawlrpc.CrawlExchangeClient, io.Closer, error) {
		connection, err := grpc.NewClient(
			address,
			grpc.WithTransportCredentials(clientCredentials),
			grpc.WithContextDialer(dialer.DialContext),
			grpc.WithConnectParams(grpc.ConnectParams{
				Backoff: backoff.Config{
					BaseDelay:  time.Millisecond,
					Multiplier: 1,
					Jitter:     0,
					MaxDelay:   time.Millisecond,
				},
				MinConnectTimeout: time.Second,
			}),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("create readiness exchange client: %w", err)
		}

		return crawlrpc.NewCrawlExchangeClient(connection), connection, nil
	}
	t.Cleanup(func() { newCrawlerExchange = previous })
}

func awaitRuntimePolicySignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal(message)
	}
}

func receiveRuntimePolicyRead(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("runtime policy read did not finish")
		return errors.New("runtime policy read timed out")
	}
}
