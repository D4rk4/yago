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

func TestRunServiceDrivesOrdersToIngest(t *testing.T) {
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

	batch := fetchOneIngest(t, js, cfg.IngestSubject)
	if string(batch.Provenance) != "admin" {
		t.Errorf("batch provenance = %q, want admin", batch.Provenance)
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

func fetchOneIngest(
	t *testing.T,
	js jetstream.JetStream,
	subject string,
) yacycrawlcontract.IngestBatch {
	t.Helper()
	stream, err := js.Stream(context.Background(), yacycrawlcontract.IngestStreamName)
	if err != nil {
		t.Fatalf("lookup ingest stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create ingest consumer: %v", err)
	}
	msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(15*time.Second))
	if err != nil {
		t.Fatalf("fetch ingest: %v", err)
	}
	msg, ok := <-msgs.Messages()
	if !ok {
		if err := msgs.Error(); err != nil {
			t.Fatalf("fetch error: %v", err)
		}
		t.Fatal("no ingest batch received")
	}
	batch, err := yacycrawlcontract.UnmarshalIngestBatch(msg.Data())
	if err != nil {
		t.Fatalf("decode ingest: %v", err)
	}
	if err := msg.Ack(); err != nil {
		t.Fatalf("ack: %v", err)
	}
	return batch
}
