package crawledpageindex_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawledpageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	testOrdersSubject    = "yacy.crawl.test.orders"
	testPageIndexSubject = "yacy.crawl.test.page-index"
)

func testCrawledPageIndex(url string) crawledpageindex.CrawledPageIndex {
	return crawledpageindex.CrawledPageIndex{
		SourceURL:     url,
		Provenance:    []byte("admin"),
		ProfileHandle: "abcdef012345",
		Postings: []yacymodel.RWIPosting{
			{
				WordHash:   yacymodel.Hash("wordhash0123"),
				Properties: map[string]string{"u": "urlhash01234"},
			},
		},
	}
}

func TestNATSCrawledPageIndexPublisherDelivers(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
		ctx,
		js,
		yacycrawlcontract.CrawledPageIndexStreamSpec{
			Subject: testPageIndexSubject,
			MaxMsgs: 64,
		},
	); err != nil {
		t.Fatalf("ensure crawled page index stream: %v", err)
	}

	publisher := crawledpageindex.NewNATSCrawledPageIndexPublisher(js, testPageIndexSubject)
	const count = 5
	want := make([]crawledpageindex.CrawledPageIndex, 0, count)
	for i := range count {
		index := testCrawledPageIndex("https://example.org/" + string(rune('a'+i)))
		want = append(want, index)
		if err := publisher.Publish(ctx, index); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	got := drainCrawledPageIndex(t, js, count)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("delivered indexes mismatch:\nwant %#v\ngot  %#v", want, got)
	}
}

func TestNATSCrawledPageIndexPublisherBackpressureBlocksThenDrains(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	ctx := context.Background()
	if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
		ctx,
		js,
		yacycrawlcontract.CrawledPageIndexStreamSpec{
			Subject: testPageIndexSubject,
			MaxMsgs: 1,
		},
	); err != nil {
		t.Fatalf("ensure streams: %v", err)
	}
	publisher := crawledpageindex.NewNATSCrawledPageIndexPublisher(js, testPageIndexSubject)

	if err := publisher.Publish(
		ctx,
		testCrawledPageIndex("https://example.org/first"),
	); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	blocked := make(chan error, 1)
	go func() {
		blocked <- publisher.Publish(ctx, testCrawledPageIndex("https://example.org/second"))
	}()

	select {
	case err := <-blocked:
		t.Fatalf("second publish returned %v before stream drained, want blocking", err)
	case <-time.After(300 * time.Millisecond):
	}

	drainCrawledPageIndex(t, js, 1)

	select {
	case err := <-blocked:
		if err != nil {
			t.Fatalf("second publish after drain: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second publish did not unblock after drain")
	}
}

func TestNATSCrawledPageIndexPublisherBackpressureRespectsDeadline(t *testing.T) {
	js := connectJetStream(t, startNATS(t))
	if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
		context.Background(),
		js,
		yacycrawlcontract.CrawledPageIndexStreamSpec{
			Subject: testPageIndexSubject,
			MaxMsgs: 1,
		},
	); err != nil {
		t.Fatalf("ensure crawled page index stream: %v", err)
	}
	publisher := crawledpageindex.NewNATSCrawledPageIndexPublisher(js, testPageIndexSubject)
	if err := publisher.Publish(
		context.Background(),
		testCrawledPageIndex("https://example.org/first"),
	); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := publisher.Publish(
		ctx,
		testCrawledPageIndex("https://example.org/second"),
	); err == nil {
		t.Fatal("expected deadline error on saturated crawled page index stream, got nil")
	}
}

func TestNATSCrawledPageIndexPublisherReportsOversizedAsTerminal(t *testing.T) {
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port:       -1,
		JetStream:  true,
		StoreDir:   t.TempDir(),
		MaxPayload: 1024,
	})
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(srv.Shutdown)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("init jetstream: %v", err)
	}

	ctx := context.Background()
	if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
		ctx,
		js,
		yacycrawlcontract.CrawledPageIndexStreamSpec{Subject: testPageIndexSubject, MaxMsgs: 64},
	); err != nil {
		t.Fatalf("ensure crawled page index stream: %v", err)
	}

	index := testCrawledPageIndex("https://example.org/oversized")
	for i := range 256 {
		index.Postings = append(index.Postings, yacymodel.RWIPosting{
			WordHash:   yacymodel.Hash("wordhash0123"),
			Properties: map[string]string{"u": "urlhash01234", "n": string(rune('a' + i%26))},
		})
	}

	publisher := crawledpageindex.NewNATSCrawledPageIndexPublisher(js, testPageIndexSubject)
	err = publisher.Publish(ctx, index)
	if !errors.Is(err, crawledpageindex.ErrCrawledPageIndexOversized) {
		t.Fatalf("oversized publish error = %v, want ErrCrawledPageIndexOversized", err)
	}
}

func drainCrawledPageIndex(
	t *testing.T,
	js jetstream.JetStream,
	count int,
) []crawledpageindex.CrawledPageIndex {
	t.Helper()
	stream, err := js.Stream(context.Background(), yacycrawlcontract.CrawledPageIndexStreamName)
	if err != nil {
		t.Fatalf("lookup crawled page index stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create drain consumer: %v", err)
	}
	out := make([]crawledpageindex.CrawledPageIndex, 0, count)
	for len(out) < count {
		msgs, err := consumer.Fetch(count-len(out), jetstream.FetchMaxWait(2*time.Second))
		if err != nil {
			t.Fatalf("fetch crawled page index: %v", err)
		}
		for msg := range msgs.Messages() {
			index, err := yacycrawlcontract.UnmarshalCrawledPageIndex(msg.Data())
			if err != nil {
				t.Fatalf("decode crawled page index: %v", err)
			}
			out = append(out, index)
			if err := msg.Ack(); err != nil {
				t.Fatalf("ack: %v", err)
			}
		}
		if err := msgs.Error(); err != nil {
			t.Fatalf("fetch error: %v", err)
		}
	}
	return out
}
