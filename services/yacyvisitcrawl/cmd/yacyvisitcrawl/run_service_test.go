package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacyvisitcrawl/internal/visitmetrics"
)

func TestRunServiceRedirectsAndPlacesOrder(t *testing.T) {
	natsURL := startTestNATS(t)
	cfg := ServiceConfig{
		NATSURL:       natsURL,
		OrdersSubject: DefaultOrdersSubject,
		ListenAddr:    freeAddr(t),
		OpsAddr:       freeAddr(t),
		OrderTimeout:  DefaultOrderTimeout,
		MaxInFlight:   DefaultMaxInFlight,
		MaxBodyBytes:  DefaultMaxBodyBytes,
		CrawlProfile: yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
			Scope:           yacycrawlcontract.ScopeDomain,
			URLMustMatch:    yacycrawlcontract.MatchAll,
			MaxPagesPerHost: DefaultCrawlMaxPagesPerHost,
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serviceErr := make(chan error, 1)
	go func() { serviceErr <- RunService(ctx, cfg, visitmetrics.New()) }()

	consumer := ordersConsumer(t, ctx, natsURL)
	waitForListening(t, cfg.ListenAddr)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	visitReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("http://%s/visit?url=https%%3A%%2F%%2Fexample.org%%2Fa", cfg.ListenAddr),
		nil,
	)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(visitReq)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://example.org/a" {
		t.Fatalf("location = %q", got)
	}

	order := nextPlacedOrder(t, consumer)
	if len(order.SeedURLs) != 1 || order.SeedURLs[0] != "https://example.org/a" {
		t.Fatalf("order seeds = %v", order.SeedURLs)
	}

	cancel()
	if err := <-serviceErr; err != nil {
		t.Fatalf("RunService: %v", err)
	}
}

func TestRunServiceRejectsBadNATSURL(t *testing.T) {
	cfg := ServiceConfig{
		NATSURL: "nats://127.0.0.1:1", ListenAddr: "127.0.0.1:0", OpsAddr: "127.0.0.1:0",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := RunService(ctx, cfg, visitmetrics.New())
	if err == nil {
		t.Fatal("unreachable nats should fail")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected connect failure, got deadline exceeded")
	}
}

func startTestNATS(t *testing.T) string {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port: -1, JetStream: true, StoreDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats not ready")
	}
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

func ordersConsumer(t *testing.T, ctx context.Context, natsURL string) jetstream.Consumer {
	t.Helper()
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	if err := yacycrawlcontract.EnsureOrdersStream(ctx, js,
		yacycrawlcontract.OrdersStreamSpec{Subject: DefaultOrdersSubject}); err != nil {
		t.Fatal(err)
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, yacycrawlcontract.OrdersStreamName,
		jetstream.ConsumerConfig{
			AckPolicy:     jetstream.AckExplicitPolicy,
			FilterSubject: DefaultOrdersSubject,
		})
	if err != nil {
		t.Fatal(err)
	}
	return consumer
}

func nextPlacedOrder(t *testing.T, consumer jetstream.Consumer) yacycrawlcontract.CrawlOrder {
	t.Helper()
	msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
	if err != nil {
		t.Fatalf("fetch placed order: %v", err)
	}
	order, err := yacycrawlcontract.UnmarshalCrawlOrder(msg.Data())
	if err != nil {
		t.Fatalf("decode order: %v", err)
	}
	return order
}

func waitForListening(t *testing.T, addr string) {
	t.Helper()
	dialer := &net.Dialer{}
	waitFor(t, func() bool {
		conn, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	})
}

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().String()
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}
