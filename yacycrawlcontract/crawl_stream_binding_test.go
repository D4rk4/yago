package yacycrawlcontract_test

import (
	"context"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
)

func TestEnsureStreamsCreatesBoundedStreams(t *testing.T) {
	js := connectJetStream(t, startNATS(t))

	spec := yacycrawlcontract.StreamSpec{
		OrdersSubject: "yacy.crawl.orders",
		IngestSubject: "yacy.crawl.ingest",
		IngestMaxMsgs: 8,
	}
	if err := yacycrawlcontract.EnsureStreams(context.Background(), js, spec); err != nil {
		t.Fatalf("ensure streams: %v", err)
	}

	orders, err := js.Stream(context.Background(), yacycrawlcontract.OrdersStreamName)
	if err != nil {
		t.Fatalf("orders stream: %v", err)
	}
	if got := orders.CachedInfo().Config.Retention; got != jetstream.WorkQueuePolicy {
		t.Fatalf("orders retention = %v, want WorkQueuePolicy", got)
	}

	ingest, err := js.Stream(context.Background(), yacycrawlcontract.IngestStreamName)
	if err != nil {
		t.Fatalf("ingest stream: %v", err)
	}
	cfg := ingest.CachedInfo().Config
	if cfg.MaxMsgs != spec.IngestMaxMsgs {
		t.Fatalf("ingest MaxMsgs = %d, want %d", cfg.MaxMsgs, spec.IngestMaxMsgs)
	}
	if cfg.Discard != jetstream.DiscardNew {
		t.Fatalf("ingest discard = %v, want DiscardNew", cfg.Discard)
	}
}

func TestEnsureStreamsIsIdempotent(t *testing.T) {
	js := connectJetStream(t, startNATS(t))

	spec := yacycrawlcontract.StreamSpec{
		OrdersSubject: "yacy.crawl.orders",
		IngestSubject: "yacy.crawl.ingest",
		IngestMaxMsgs: 8,
	}
	if err := yacycrawlcontract.EnsureStreams(context.Background(), js, spec); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := yacycrawlcontract.EnsureStreams(context.Background(), js, spec); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestEnsureStreamsReportsBrokerFailure(t *testing.T) {
	url := startNATS(t)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("init jetstream: %v", err)
	}
	nc.Close()

	spec := yacycrawlcontract.StreamSpec{
		OrdersSubject: "yacy.crawl.orders",
		IngestSubject: "yacy.crawl.ingest",
		IngestMaxMsgs: 8,
	}
	if err := yacycrawlcontract.EnsureStreams(context.Background(), js, spec); err == nil {
		t.Fatal("ensure streams on closed connection should fail")
	}
}

func TestEnsureStreamsReportsIngestStreamFailure(t *testing.T) {
	js := connectJetStream(t, startNATS(t))

	spec := yacycrawlcontract.StreamSpec{
		OrdersSubject: "yacy.crawl.orders",
		IngestSubject: "",
		IngestMaxMsgs: 8,
	}
	if err := yacycrawlcontract.EnsureStreams(context.Background(), js, spec); err == nil {
		t.Fatal("invalid ingest stream subject should fail")
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
