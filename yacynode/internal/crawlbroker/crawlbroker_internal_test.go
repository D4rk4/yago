package crawlbroker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
)

type fakeConsumer struct{}

func (fakeConsumer) Consume(
	jetstream.MessageHandler,
	...jetstream.PullConsumeOpt,
) (jetstream.ConsumeContext, error) {
	return fakeConsumeContext{}, nil
}

type fakeConsumeContext struct {
	stopped chan<- struct{}
}

func (c fakeConsumeContext) Stop() {
	if c.stopped != nil {
		c.stopped <- struct{}{}
	}
}

func (fakeConsumeContext) Drain() {}

func (fakeConsumeContext) Closed() <-chan struct{} {
	closed := make(chan struct{})
	close(closed)
	return closed
}

type fakeMsg struct {
	data []byte
	nak  bool
	term bool
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *fakeMsg) Data() []byte                              { return m.data }
func (m *fakeMsg) Headers() nats.Header                      { return nil }
func (m *fakeMsg) Subject() string                           { return "" }
func (m *fakeMsg) Reply() string                             { return "" }
func (m *fakeMsg) Ack() error                                { return nil }
func (m *fakeMsg) DoubleAck(context.Context) error           { return nil }
func (m *fakeMsg) Nak() error                                { m.nak = true; return nil }
func (m *fakeMsg) NakWithDelay(time.Duration) error          { return nil }
func (m *fakeMsg) InProgress() error                         { return nil }
func (m *fakeMsg) Term() error                               { m.term = true; return nil }
func (m *fakeMsg) TermWithReason(string) error               { return nil }

func restoreBrokerSeams(t *testing.T) {
	t.Helper()
	savedConnect := connectNATS
	savedClose := closeNATS
	savedJetStream := newJetStream
	savedEnsure := ensureCrawlStreams
	savedIngest := openIngestReceiver
	savedMarshal := marshalCrawlOrder
	savedPublish := publishCrawlOrder
	savedCreate := createIngestConsumer
	savedConsume := consumeIngestMessages
	t.Cleanup(func() {
		connectNATS = savedConnect
		closeNATS = savedClose
		newJetStream = savedJetStream
		ensureCrawlStreams = savedEnsure
		openIngestReceiver = savedIngest
		marshalCrawlOrder = savedMarshal
		publishCrawlOrder = savedPublish
		createIngestConsumer = savedCreate
		consumeIngestMessages = savedConsume
	})
}

func TestOpenReturnsSetupErrors(t *testing.T) {
	restoreBrokerSeams(t)
	sentinel := errors.New("connect failed")
	connectNATS = func(string, ...nats.Option) (*nats.Conn, error) { return nil, sentinel }
	closeNATS = func(*nats.Conn) {}
	if _, err := Open(t.Context(), Config{}); !errors.Is(err, sentinel) {
		t.Fatalf("Open error = %v, want %v", err, sentinel)
	}

	connectNATS = func(string, ...nats.Option) (*nats.Conn, error) { return &nats.Conn{}, nil }
	sentinel = errors.New("jetstream failed")
	newJetStream = func(*nats.Conn, ...jetstream.JetStreamOpt) (jetstream.JetStream, error) {
		return nil, sentinel
	}
	if _, err := Open(t.Context(), Config{}); !errors.Is(err, sentinel) {
		t.Fatalf("Open error = %v, want %v", err, sentinel)
	}

	newJetStream = func(*nats.Conn, ...jetstream.JetStreamOpt) (jetstream.JetStream, error) {
		return nil, nil
	}
	sentinel = errors.New("ensure failed")
	ensureCrawlStreams = func(context.Context, jetstream.JetStream, yacycrawlcontract.StreamSpec) error {
		return sentinel
	}
	if _, err := Open(t.Context(), Config{}); !errors.Is(err, sentinel) {
		t.Fatalf("Open error = %v, want %v", err, sentinel)
	}

	ensureCrawlStreams = func(context.Context, jetstream.JetStream, yacycrawlcontract.StreamSpec) error {
		return nil
	}
	sentinel = errors.New("ingest failed")
	openIngestReceiver = func(context.Context, jetstream.JetStream, string, string) (*IngestReceiver, error) {
		return nil, sentinel
	}
	if _, err := Open(t.Context(), Config{}); !errors.Is(err, sentinel) {
		t.Fatalf("Open error = %v, want %v", err, sentinel)
	}
}

func TestOrderPublisherReturnsEncodeAndPublishErrors(t *testing.T) {
	restoreBrokerSeams(t)
	sentinel := errors.New("encode failed")
	marshalCrawlOrder = func(yacycrawlcontract.CrawlOrder) ([]byte, error) {
		return nil, sentinel
	}
	if err := (&OrderPublisher{}).Publish(
		t.Context(),
		yacycrawlcontract.CrawlOrder{},
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("Publish error = %v, want %v", err, sentinel)
	}

	marshalCrawlOrder = func(yacycrawlcontract.CrawlOrder) ([]byte, error) {
		return []byte("{}"), nil
	}
	sentinel = errors.New("publish failed")
	publishCrawlOrder = func(context.Context, crawlOrderPublisher, string, []byte) error {
		return sentinel
	}
	if err := (&OrderPublisher{}).Publish(
		t.Context(),
		yacycrawlcontract.CrawlOrder{},
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("Publish error = %v, want %v", err, sentinel)
	}
}

func TestNewIngestReceiverReturnsSetupErrors(t *testing.T) {
	restoreBrokerSeams(t)
	sentinel := errors.New("create failed")
	createIngestConsumer = func(context.Context, jetstream.JetStream, string, string) (ingestConsumer, error) {
		return nil, sentinel
	}
	if _, err := newIngestReceiver(
		t.Context(),
		nil,
		"durable",
		"subject",
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("newIngestReceiver error = %v, want %v", err, sentinel)
	}

	createIngestConsumer = func(context.Context, jetstream.JetStream, string, string) (ingestConsumer, error) {
		return fakeConsumer{}, nil
	}
	sentinel = errors.New("consume failed")
	consumeIngestMessages = func(ingestConsumer, jetstream.MessageHandler) (jetstream.ConsumeContext, error) {
		return nil, sentinel
	}
	if _, err := newIngestReceiver(
		t.Context(),
		nil,
		"durable",
		"subject",
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("newIngestReceiver error = %v, want %v", err, sentinel)
	}
}

func TestNewIngestReceiverNaksWhenContextEndsBeforeDelivery(t *testing.T) {
	restoreBrokerSeams(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msgData, err := yacycrawlcontract.MarshalIngestBatch(
		yacycrawlcontract.IngestBatch{SourceURL: "https://example.org"},
	)
	if err != nil {
		t.Fatal(err)
	}
	msg := &fakeMsg{data: msgData}
	stopped := make(chan struct{}, 1)
	createIngestConsumer = func(context.Context, jetstream.JetStream, string, string) (ingestConsumer, error) {
		return fakeConsumer{}, nil
	}
	consumeIngestMessages = func(_ ingestConsumer, handler jetstream.MessageHandler) (jetstream.ConsumeContext, error) {
		handler(msg)
		return fakeConsumeContext{stopped: stopped}, nil
	}

	if _, err := newIngestReceiver(ctx, nil, "durable", "subject"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if !msg.nak {
		t.Fatal("message was not nacked after context cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("consume context was not stopped")
	}
}

func TestIngestDeliveryNakCallsMessageNak(t *testing.T) {
	restoreBrokerSeams(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgData, err := yacycrawlcontract.MarshalIngestBatch(
		yacycrawlcontract.IngestBatch{SourceURL: "https://example.org"},
	)
	if err != nil {
		t.Fatal(err)
	}
	msg := &fakeMsg{data: msgData}
	createIngestConsumer = func(context.Context, jetstream.JetStream, string, string) (ingestConsumer, error) {
		return fakeConsumer{}, nil
	}
	consumeIngestMessages = func(_ ingestConsumer, handler jetstream.MessageHandler) (jetstream.ConsumeContext, error) {
		go handler(msg)
		return fakeConsumeContext{}, nil
	}

	receiver, err := newIngestReceiver(ctx, nil, "durable", "subject")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case delivery := <-receiver.Receive():
		if err := delivery.Nak(ctx); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("delivery was not received")
	}
	if !msg.nak {
		t.Fatal("message Nak was not called")
	}
}
