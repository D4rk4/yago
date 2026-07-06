package main

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlmetrics"
)

func TestRunServiceProcessesOrderThenStops(t *testing.T) {
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
	defer srv.Shutdown()

	proxy, _ := url.Parse("http://127.0.0.1:1")
	cfg := ServiceConfig{
		NATSURL:            srv.ClientURL(),
		OrdersSubject:      DefaultOrdersSubject,
		OrdersDurable:      DefaultOrdersDurable,
		PageIndexSubject:   DefaultPageIndexSubject,
		PageIndexMaxMsgs:   DefaultMaxMsgs,
		PagesSubject:       DefaultPagesSubject,
		PagesMaxMsgs:       DefaultMaxMsgs,
		ProxyURL:           proxy,
		FetchConcurrency:   2,
		IndexOutputEnabled: true,
		RunPageBudget:      DefaultRunPageBudget,
		FrontierCap:        DefaultFrontierCap,
		MaxBodyBytes:       DefaultMaxBodyBytes,
		FetchDeadline:      time.Second,
		OpsAddr:            "127.0.0.1:0",
	}

	publishOrder(t, cfg.NATSURL)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := RunService(
		ctx,
		cfg,
		crawlmetrics.New(),
	); err != nil &&
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunService: %v", err)
	}
}

func TestRunServiceFailsOnEmptyExtractor(t *testing.T) {
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
	defer srv.Shutdown()

	proxy, _ := url.Parse("http://127.0.0.1:1")
	cfg := ServiceConfig{
		NATSURL: srv.ClientURL(), OrdersSubject: DefaultOrdersSubject,
		OrdersDurable: DefaultOrdersDurable, PageIndexSubject: DefaultPageIndexSubject,
		PageIndexMaxMsgs: DefaultMaxMsgs, PagesSubject: DefaultPagesSubject,
		PagesMaxMsgs: DefaultMaxMsgs, ProxyURL: proxy, FetchConcurrency: 2,
		IndexOutputEnabled: true, MaxBodyBytes: DefaultMaxBodyBytes,
		FetchDeadline: time.Second, OpsAddr: "127.0.0.1:0",
		ContentTypes: []string{"application/unregistered"},
	}
	if err := RunService(context.Background(), cfg, crawlmetrics.New()); err == nil {
		t.Fatal("empty active extractor set should fail startup")
	}
}

func TestRunServiceRejectsBadNATSURL(t *testing.T) {
	cfg := ServiceConfig{NATSURL: "nats://127.0.0.1:1", FetchConcurrency: 2, OpsAddr: "127.0.0.1:0"}
	if err := RunService(context.Background(), cfg, crawlmetrics.New()); err == nil {
		t.Fatal("unreachable nats should fail")
	}
}

func publishOrder(t *testing.T, natsURL string) {
	t.Helper()
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureOrdersStream(ctx, js,
		yacycrawlcontract.OrdersStreamSpec{Subject: DefaultOrdersSubject}); err != nil {
		t.Fatal(err)
	}
	payload, err := yacycrawlcontract.MarshalCrawlOrder(yacycrawlcontract.CrawlOrder{
		OrderID: "o1",
		Profile: yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
			Scope: yacycrawlcontract.ScopeWide, URLMustMatch: yacycrawlcontract.MatchAll,
			MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
		}),
		SeedURLs: []string{"http://origin.example/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.Publish(ctx, DefaultOrdersSubject, payload); err != nil {
		t.Fatal(err)
	}
}
