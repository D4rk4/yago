package pageintake_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/pageintake"
)

type fakeMsg struct {
	data  []byte
	acked chan string
}

func (m *fakeMsg) Subject() string                  { return "subj" }
func (m *fakeMsg) Reply() string                    { return "" }
func (m *fakeMsg) Data() []byte                     { return m.data }
func (m *fakeMsg) Headers() nats.Header             { return nil }
func (m *fakeMsg) Ack() error                       { m.acked <- "ack"; return nil }
func (m *fakeMsg) DoubleAck(context.Context) error  { m.acked <- "ack"; return nil }
func (m *fakeMsg) Nak() error                       { m.acked <- "nak"; return nil }
func (m *fakeMsg) NakWithDelay(time.Duration) error { m.acked <- "nak"; return nil }
func (m *fakeMsg) InProgress() error                { return nil }
func (m *fakeMsg) Term() error                      { m.acked <- "term"; return nil }
func (m *fakeMsg) TermWithReason(string) error      { m.acked <- "term"; return nil }

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return &jetstream.MsgMetadata{}, nil }

type fakeIterator struct {
	messages []jetstream.Msg
	mu       sync.Mutex
	stopped  bool
}

func (it *fakeIterator) Next(...jetstream.NextOpt) (jetstream.Msg, error) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if len(it.messages) == 0 {
		return nil, jetstream.ErrMsgIteratorClosed
	}
	msg := it.messages[0]
	it.messages = it.messages[1:]
	return msg, nil
}

func (it *fakeIterator) Stop() {
	it.mu.Lock()
	defer it.mu.Unlock()
	it.stopped = true
}

func (it *fakeIterator) Drain() {}

type fakeSource struct {
	iterator *fakeIterator
}

func (s fakeSource) Messages(...jetstream.PullMessagesOpt) (jetstream.MessagesContext, error) {
	return s.iterator, nil
}

type recordingIndexer struct {
	fail bool
}

func (r recordingIndexer) Index(context.Context, yacycrawlcontract.CrawledPage) error {
	if r.fail {
		return errors.New("index failed")
	}
	return nil
}

func newFakeMsg(data []byte, acked chan string) *fakeMsg {
	return &fakeMsg{data: data, acked: acked}
}

func TestCrawledPageConsumerAcksOnSuccessfulIndex(t *testing.T) {
	acked := make(chan string, 1)
	data, err := yacycrawlcontract.MarshalCrawledPage(
		yacycrawlcontract.CrawledPage{CanonicalURL: "https://example.com/"},
	)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	source := fakeSource{
		iterator: &fakeIterator{messages: []jetstream.Msg{newFakeMsg(data, acked)}},
	}
	consumer := pageintake.NewCrawledPageConsumer(source, recordingIndexer{}, 1)

	if err := consumer.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	select {
	case action := <-acked:
		if action != "ack" {
			t.Errorf("action = %q, want ack", action)
		}
	default:
		t.Fatal("expected message to be acked")
	}
}

func TestCrawledPageConsumerNaksOnIndexFailure(t *testing.T) {
	acked := make(chan string, 1)
	data, err := yacycrawlcontract.MarshalCrawledPage(
		yacycrawlcontract.CrawledPage{CanonicalURL: "https://example.com/"},
	)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	source := fakeSource{
		iterator: &fakeIterator{messages: []jetstream.Msg{newFakeMsg(data, acked)}},
	}
	consumer := pageintake.NewCrawledPageConsumer(source, recordingIndexer{fail: true}, 1)

	if err := consumer.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	select {
	case action := <-acked:
		if action != "nak" {
			t.Errorf("action = %q, want nak", action)
		}
	default:
		t.Fatal("expected message to be naked")
	}
}

func TestCrawledPageConsumerTermsOnDecodeFailure(t *testing.T) {
	acked := make(chan string, 1)
	source := fakeSource{
		iterator: &fakeIterator{messages: []jetstream.Msg{newFakeMsg([]byte("not json"), acked)}},
	}
	consumer := pageintake.NewCrawledPageConsumer(source, recordingIndexer{}, 1)

	if err := consumer.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	select {
	case action := <-acked:
		if action != "term" {
			t.Errorf("action = %q, want term", action)
		}
	default:
		t.Fatal("expected message to be termed")
	}
}
