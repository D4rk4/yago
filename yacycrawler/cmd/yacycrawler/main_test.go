package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func TestServiceIsOrderDrivenEndToEnd(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write(
			[]byte(`<html lang="en"><title>Hi</title><body>words here</body></html>`),
		); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer origin.Close()

	url := startNATS(t)
	t.Setenv(yacycrawler.EnvNATSURL, url)
	t.Setenv(yacycrawler.EnvWorkers, "1")

	cfg, err := yacycrawler.LoadServiceConfig(os.Getenv)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- run(ctx, cfg) }()

	js := connectJetStream(t, url)
	waitForStream(t, js, yacycrawler.OrdersStreamName)

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
		{URL: origin.URL, ProfileHandle: order.Profile.Handle},
	}
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	if _, err := js.Publish(ctx, cfg.OrdersSubject, data); err != nil {
		t.Fatalf("publish order: %v", err)
	}

	batch := fetchOneIngest(t, js)
	if batch.ProfileHandle != order.Profile.Handle {
		t.Errorf("batch handle = %q, want %q", batch.ProfileHandle, order.Profile.Handle)
	}
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

func startNATS(t *testing.T) string {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

func connectJetStream(t *testing.T, url string) jetstream.JetStream {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("init jetstream: %v", err)
	}
	return js
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

func fetchOneIngest(t *testing.T, js jetstream.JetStream) yacycrawlcontract.IngestBatch {
	t.Helper()
	stream, err := js.Stream(context.Background(), yacycrawler.IngestStreamName)
	if err != nil {
		t.Fatalf("lookup ingest stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{
		AckPolicy: jetstream.AckExplicitPolicy,
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
