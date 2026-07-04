package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func TestRunServiceDrivesOrdersToCrawledPageIndex(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no robots", http.StatusNotFound)
	}))
	defer origin.Close()

	url := startNATS(t)
	cfg := serviceConfig(url)

	source := htmlPageSource(map[string]string{"/": "words here"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- RunService(ctx, cfg, source) }()

	js := connectJetStream(t, url)
	waitForStream(t, js, yacycrawlcontract.OrdersStreamName)

	publishDefaultOrder(t, ctx, js, cfg.OrdersSubject, origin.URL)

	index := fetchOneCrawledPageIndex(t, js, cfg.CrawledPageIndexSubject)
	if string(index.Provenance) != "admin" {
		t.Errorf("index provenance = %q, want admin", index.Provenance)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("service did not shut down after cancel")
	}
}

func serviceConfig(natsURL string) ServiceConfig {
	getenv := func(key string) string {
		switch key {
		case EnvNATSURL:
			return natsURL
		case EnvProxyURL:
			return "http://127.0.0.1:1"
		case EnvWorkers:
			return "1"
		default:
			return ""
		}
	}
	cfg, err := LoadServiceConfig(getenv)
	if err != nil {
		panic(err)
	}
	return cfg
}

func publishDefaultOrder(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
	subject, target string,
) {
	t.Helper()
	order := yacycrawlcontract.CrawlOrder{
		OrderID:    "c0ffee00-1122-4334-8556-778899aabbcc",
		Provenance: []byte("admin"),
		Profile: yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
			Name:            "default",
			Scope:           yacycrawlcontract.ScopeDomain,
			URLMustMatch:    yacycrawlcontract.MatchAll,
			MaxDepth:        0,
			MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
		}),
	}
	order.Requests = []yacycrawlcontract.CrawlRequest{
		{URL: target, ProfileHandle: order.Profile.Handle},
	}
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	if _, err := js.Publish(ctx, subject, data); err != nil {
		t.Fatalf("publish order: %v", err)
	}
}

func waitForStream(t *testing.T, js jetstream.JetStream, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := js.Stream(context.Background(), name); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("stream %s not created in time", name)
}

func fetchOneCrawledPageIndex(
	t *testing.T,
	js jetstream.JetStream,
	subject string,
) yacycrawlcontract.CrawledPageIndex {
	t.Helper()
	stream, err := js.Stream(context.Background(), yacycrawlcontract.CrawledPageIndexStreamName)
	if err != nil {
		t.Fatalf("lookup crawled page index stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create crawled page index consumer: %v", err)
	}
	msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(15*time.Second))
	if err != nil {
		t.Fatalf("fetch crawled page index: %v", err)
	}
	msg, ok := <-msgs.Messages()
	if !ok {
		if err := msgs.Error(); err != nil {
			t.Fatalf("fetch error: %v", err)
		}
		t.Fatal("no crawled page index received")
	}
	index, err := yacycrawlcontract.UnmarshalCrawledPageIndex(msg.Data())
	if err != nil {
		t.Fatalf("decode crawled page index: %v", err)
	}
	if err := msg.Ack(); err != nil {
		t.Fatalf("ack: %v", err)
	}
	return index
}
