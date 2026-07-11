package crawlresults_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type recordingAnchorReceiver struct {
	recordingDocumentReceiver
	update documentstore.AnchorUpdate
	err    error
	sets   []documentstore.OutboundAnchorSet
}

func (r *recordingAnchorReceiver) ReplaceOutboundAnchors(
	_ context.Context,
	sets []documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	r.sets = append([]documentstore.OutboundAnchorSet(nil), sets...)

	return r.update, r.err
}

func TestSingleIngestRedeliversWhenInboundAnchorStoreFails(t *testing.T) {
	receiver := &recordingAnchorReceiver{err: errors.New("anchor store failed")}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	counter := &settleCounter{}
	batch := groupDocBatch("https://source.example/", "source text")
	batch.Document.OutboundAnchors = []yagocrawlcontract.OutboundAnchor{{
		TargetURL: "https://target.example/",
		Text:      "target",
	}}
	stream.out <- groupDelivery(batch, &wg, counter, nil)
	consumer := crawlresults.NewIngestConsumer(
		stream,
		receiver,
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	drainGroup(consumer, &wg)

	if acks, naks := counter.counts(); acks != 0 || naks != 1 {
		t.Fatalf("acks/naks = %d/%d", acks, naks)
	}
	if len(receiver.sets) != 1 ||
		receiver.sets[0].Anchors[0].TargetURL != "https://target.example/" {
		t.Fatalf("anchor sets = %#v", receiver.sets)
	}
}

func TestGroupedIngestRedeliversWhenInboundAnchorStoreIsBusy(t *testing.T) {
	receiver := &recordingAnchorReceiver{update: documentstore.AnchorUpdate{Busy: true}}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 2)}
	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(
		groupDocBatch("https://source.example/a", "first source"),
		&wg,
		counter,
		nil,
	)
	stream.out <- groupDelivery(
		groupDocBatch("https://source.example/b", "second source"),
		&wg,
		counter,
		nil,
	)
	consumer := crawlresults.NewIngestConsumer(
		stream,
		receiver,
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	drainGroup(consumer, &wg)

	if acks, naks := counter.counts(); acks != 0 || naks != 2 {
		t.Fatalf("acks/naks = %d/%d", acks, naks)
	}
	if len(receiver.sets) != 2 {
		t.Fatalf("anchor sets = %#v", receiver.sets)
	}
}

func TestRemovalRedeliversWhenOutboundAnchorClearFails(t *testing.T) {
	receiver := &recordingAnchorReceiver{err: errors.New("anchor clear failed")}
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	var wg sync.WaitGroup
	counter := &settleCounter{}
	stream.out <- groupDelivery(
		yagocrawlcontract.IngestBatch{SourceURL: removalURL, Removed: true},
		&wg,
		counter,
		nil,
	)
	consumer := crawlresults.NewIngestConsumer(
		stream,
		receiver,
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	drainGroup(consumer, &wg)

	if acks, naks := counter.counts(); acks != 0 || naks != 1 {
		t.Fatalf("acks/naks = %d/%d", acks, naks)
	}
	if len(receiver.sets) != 1 || receiver.sets[0].SourceURL != removalURL ||
		len(receiver.sets[0].Anchors) != 0 {
		t.Fatalf("clear sets = %#v", receiver.sets)
	}
}
