package crawlorder

import (
	"context"
	"errors"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
)

const (
	internalTestOrdersSubject = "yacy.crawl.internal.orders"
	internalTestIngestSubject = "yacy.crawl.internal.ingest"
)

type recordingJetStreamMessage struct {
	data  []byte
	acks  int
	naks  int
	terms int
}

func (m *recordingJetStreamMessage) Metadata() (*jetstream.MsgMetadata, error) {
	return nil, nil
}

func (m *recordingJetStreamMessage) Data() []byte {
	return m.data
}

func (m *recordingJetStreamMessage) Headers() nats.Header {
	return nil
}

func (m *recordingJetStreamMessage) Subject() string {
	return ""
}

func (m *recordingJetStreamMessage) Reply() string {
	return ""
}

func (m *recordingJetStreamMessage) Ack() error {
	m.acks++
	return nil
}

func (m *recordingJetStreamMessage) DoubleAck(context.Context) error {
	m.acks++
	return nil
}

func (m *recordingJetStreamMessage) Nak() error {
	m.naks++
	return nil
}

func (m *recordingJetStreamMessage) NakWithDelay(time.Duration) error {
	m.naks++
	return nil
}

func (m *recordingJetStreamMessage) InProgress() error {
	return nil
}

func (m *recordingJetStreamMessage) Term() error {
	m.terms++
	return nil
}

func (m *recordingJetStreamMessage) TermWithReason(string) error {
	m.terms++
	return nil
}

func marshalInternalOrder(t *testing.T) []byte {
	t.Helper()
	data, err := yacycrawlcontract.MarshalCrawlOrder(yacycrawlcontract.CrawlOrder{
		Profile: consumerProfile(),
	})
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	return data
}

func TestDeliverCrawlOrderNaksWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msg := &recordingJetStreamMessage{data: marshalInternalOrder(t)}

	deliverCrawlOrder(ctx, make(chan CrawlOrderDelivery), msg)

	if msg.naks != 1 {
		t.Fatalf("naks = %d, want 1", msg.naks)
	}
}

func TestDeliverCrawlOrderExposesDeliveryAckFunctions(t *testing.T) {
	out := make(chan CrawlOrderDelivery, 1)
	msg := &recordingJetStreamMessage{data: marshalInternalOrder(t)}

	deliverCrawlOrder(context.Background(), out, msg)
	delivery := <-out

	if err := delivery.Nak(context.Background()); err != nil {
		t.Fatalf("nak: %v", err)
	}
	if err := delivery.Term(context.Background()); err != nil {
		t.Fatalf("term: %v", err)
	}
	if msg.naks != 1 || msg.terms != 1 {
		t.Fatalf("message counters: naks=%d terms=%d", msg.naks, msg.terms)
	}
}

func TestDeliverCrawlOrderTermsInvalidMessage(t *testing.T) {
	msg := &recordingJetStreamMessage{data: []byte("not json")}

	deliverCrawlOrder(context.Background(), make(chan CrawlOrderDelivery, 1), msg)

	if msg.terms != 1 {
		t.Fatalf("terms = %d, want 1", msg.terms)
	}
}

func TestNATSOrderReceiverReturnsConsumeError(t *testing.T) {
	savedConsume := consumeCrawlOrderMessages
	t.Cleanup(func() { consumeCrawlOrderMessages = savedConsume })
	sentinel := errors.New("consume failed")
	consumeCrawlOrderMessages = func(
		jetstream.Consumer,
		jetstream.MessageHandler,
	) (jetstream.ConsumeContext, error) {
		return nil, sentinel
	}
	js := connectInternalJetStream(t, startInternalNATS(t))
	if err := yacycrawlcontract.EnsureStreams(
		context.Background(),
		js,
		yacycrawlcontract.StreamSpec{
			OrdersSubject: internalTestOrdersSubject,
			IngestSubject: internalTestIngestSubject,
			IngestMaxMsgs: 64,
		},
	); err != nil {
		t.Fatalf("ensure streams: %v", err)
	}

	_, err := NewNATSOrderReceiver(
		context.Background(),
		js,
		"consume-error",
		internalTestOrdersSubject,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func startInternalNATS(t *testing.T) string {
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

func connectInternalJetStream(t *testing.T, url string) jetstream.JetStream {
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
