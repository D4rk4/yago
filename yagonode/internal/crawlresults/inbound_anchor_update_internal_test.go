package crawlresults

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type anchorUpdateScript struct {
	update        documentstore.AnchorUpdate
	documents     []documentstore.Document
	err           error
	finalizeErr   error
	sets          []documentstore.OutboundAnchorSet
	finalizations []documentstore.OutboundAnchorFinalization
}

func (s *anchorUpdateScript) VisitOutboundAnchorDocuments(
	_ context.Context,
	_ []documentstore.OutboundAnchorFinalization,
	visit func([]documentstore.Document) error,
) error {
	if len(s.documents) == 0 {
		return nil
	}

	return visit(s.documents)
}

func (s *anchorUpdateScript) FinalizeOutboundAnchors(
	_ context.Context,
	finalizations []documentstore.OutboundAnchorFinalization,
) error {
	s.finalizations = append(
		[]documentstore.OutboundAnchorFinalization(nil),
		finalizations...,
	)

	return s.finalizeErr
}

func (*anchorUpdateScript) ReleaseOutboundAnchors(
	[]documentstore.OutboundAnchorFinalization,
) {
}

func (*anchorUpdateScript) Receive(
	context.Context,
	[]documentstore.Document,
) (documentstore.Receipt, error) {
	return documentstore.Receipt{}, nil
}

func (s *anchorUpdateScript) ReplaceOutboundAnchors(
	_ context.Context,
	sets []documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	s.sets = append([]documentstore.OutboundAnchorSet(nil), sets...)

	return s.update, s.err
}

type anchorIndexScript struct {
	docs []documentstore.Document
	err  error
}

type blockingAnchorIndex struct {
	anchorIndexScript
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (i *blockingAnchorIndex) Index(
	_ context.Context,
	doc documentstore.Document,
) error {
	i.docs = append(i.docs, doc)
	i.once.Do(func() { close(i.entered) })
	<-i.release

	return nil
}

type channelAnchorIndex struct {
	anchorIndexScript
	documents chan documentstore.Document
}

func (i *channelAnchorIndex) Index(
	_ context.Context,
	doc documentstore.Document,
) error {
	i.documents <- doc

	return nil
}

func (s *anchorIndexScript) Index(_ context.Context, doc documentstore.Document) error {
	s.docs = append(s.docs, doc)

	return s.err
}

func (*anchorIndexScript) Delete(context.Context, string) error { return nil }

func (*anchorIndexScript) Search(
	context.Context,
	searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	return searchindex.SearchResultSet{}, nil
}

func (*anchorIndexScript) Stats(context.Context) (searchindex.IndexStats, error) {
	return searchindex.IndexStats{}, nil
}

func anchorDelivery(batch yagocrawlcontract.IngestBatch, naked *bool) IngestDelivery {
	return IngestDelivery{
		Batch: batch,
		Ack:   func(context.Context) error { return nil },
		Nak: func(context.Context) error {
			*naked = true

			return nil
		},
	}
}

func TestOutboundAnchorSetFromIngestMapsEvidenceAndFallbacks(t *testing.T) {
	doc := yagocrawlcontract.DocumentIngest{
		CanonicalURL:                "https://source.example/",
		OutboundAnchorEvidenceKnown: true,
		OutboundAnchors: []yagocrawlcontract.OutboundAnchor{{
			TargetURL:     "https://target.example/",
			Text:          "anchor",
			NoFollow:      true,
			UserGenerated: true,
			Sponsored:     true,
		}},
	}
	set, ok := outboundAnchorSetFromIngest(doc)
	if !ok || set.SourceURL != doc.CanonicalURL || len(set.Anchors) != 1 ||
		!set.Anchors[0].NoFollow || !set.Anchors[0].UserGenerated ||
		!set.Anchors[0].Sponsored {
		t.Fatalf("anchor set = %#v/%v", set, ok)
	}
	doc.NormalizedURL = "https://normalized.example/"
	set, ok = outboundAnchorSetFromIngest(doc)
	if !ok || set.SourceURL != doc.NormalizedURL {
		t.Fatalf("normalized source = %#v/%v", set, ok)
	}
	if _, ok := outboundAnchorSetFromIngest(yagocrawlcontract.DocumentIngest{}); ok {
		t.Fatal("empty document should not produce an anchor set")
	}
	doc.OutboundAnchorEvidenceKnown = false
	if _, ok := outboundAnchorSetFromIngest(doc); ok {
		t.Fatal("unknown evidence should preserve prior anchor contributions")
	}
	if _, ok := outboundAnchorSetFromIngest(yagocrawlcontract.DocumentIngest{
		OutboundAnchorEvidenceKnown: true,
	}); ok {
		t.Fatal("known evidence without a source should not produce an anchor set")
	}
}

func TestUpdateInboundAnchorsHandlesCapabilitiesAndUpdates(t *testing.T) {
	batch := yagocrawlcontract.IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		NormalizedURL:               "https://source.example/",
		OutboundAnchorEvidenceKnown: true,
	}}
	var naked bool
	delivery := anchorDelivery(batch, &naked)
	consumer := &IngestConsumer{observer: noopIngestObserver{}}
	if consumer.updateInboundAnchors(t.Context(), []IngestDelivery{delivery}) {
		t.Fatal("missing anchor capability should continue")
	}

	script := &anchorUpdateScript{}
	consumer.anchors = script
	if consumer.updateInboundAnchors(t.Context(), nil) {
		t.Fatal("empty deliveries should continue")
	}
	if consumer.updateInboundAnchors(t.Context(), []IngestDelivery{{}}) {
		t.Fatal("document without a source should continue")
	}
	if consumer.updateInboundAnchors(t.Context(), []IngestDelivery{delivery}) ||
		len(script.sets) != 1 {
		t.Fatalf("successful update = %#v", script.sets)
	}

	target := documentstore.Document{NormalizedURL: "https://target.example/"}
	script.documents = []documentstore.Document{target}
	index := &anchorIndexScript{}
	consumer.index = index
	if consumer.updateInboundAnchors(t.Context(), []IngestDelivery{delivery}) ||
		len(index.docs) != 1 || index.docs[0].NormalizedURL != target.NormalizedURL {
		t.Fatalf("indexed updates = %#v", index.docs)
	}

	consumer.index = nil
	if consumer.updateInboundAnchors(t.Context(), []IngestDelivery{delivery}) {
		t.Fatal("nil index should continue after persistence")
	}
}

func TestReplaceOutboundAnchorsRedeliversFailures(t *testing.T) {
	batch := yagocrawlcontract.IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		NormalizedURL:               "https://source.example/",
		OutboundAnchorEvidenceKnown: true,
	}}
	tests := map[string]struct {
		update      documentstore.AnchorUpdate
		documents   []documentstore.Document
		storeErr    error
		indexErr    error
		finalizeErr error
	}{
		"store error": {storeErr: errors.New("store failed")},
		"busy":        {update: documentstore.AnchorUpdate{Busy: true}},
		"index error": {
			documents: []documentstore.Document{{
				NormalizedURL: "https://target.example/",
			}},
			indexErr: errors.New("index failed"),
		},
		"finalization error": {
			finalizeErr: errors.New("finalization failed"),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var naked bool
			script := &anchorUpdateScript{
				update:      test.update,
				documents:   test.documents,
				err:         test.storeErr,
				finalizeErr: test.finalizeErr,
			}
			consumer := &IngestConsumer{
				anchors:  script,
				index:    &anchorIndexScript{err: test.indexErr},
				observer: noopIngestObserver{},
			}
			if !consumer.updateInboundAnchors(
				t.Context(),
				[]IngestDelivery{anchorDelivery(batch, &naked)},
			) || !naked {
				t.Fatal("anchor failure should redeliver")
			}
		})
	}
}

func TestInboundAnchorIndexFailureReplaysAfterStorageRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	directory, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	target := "https://target.example/page"
	source := "https://source.example/page"
	if _, err := receiver.Receive(
		t.Context(),
		[]documentstore.Document{{NormalizedURL: target}},
	); err != nil {
		t.Fatal(err)
	}
	batch := yagocrawlcontract.IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		NormalizedURL:               source,
		OutboundAnchorEvidenceKnown: true,
		OutboundAnchors: []yagocrawlcontract.OutboundAnchor{{
			TargetURL: target,
			Text:      "restart-safe",
		}},
	}}
	var firstNaked bool
	consumer := &IngestConsumer{
		anchors:  receiver.(documentstore.InboundAnchorReceiver),
		index:    &anchorIndexScript{err: errors.New("index interrupted")},
		observer: noopIngestObserver{},
	}
	if !consumer.updateInboundAnchors(
		t.Context(),
		[]IngestDelivery{anchorDelivery(batch, &firstNaked)},
	) || !firstNaked {
		t.Fatal("interrupted anchor index was not redelivered")
	}
	stored, found, err := directory.Document(t.Context(), target)
	if err != nil || !found || len(stored.Inlinks) != 1 {
		t.Fatalf("phase-one target = %#v/%t/%v", stored, found, err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	_, receiver, err = documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	index := &anchorIndexScript{}
	consumer = &IngestConsumer{
		anchors:  receiver.(documentstore.InboundAnchorReceiver),
		index:    index,
		observer: noopIngestObserver{},
	}
	var replayNaked bool
	delivery := anchorDelivery(batch, &replayNaked)
	if consumer.updateInboundAnchors(t.Context(), []IngestDelivery{delivery}) ||
		replayNaked || len(index.docs) != 1 || index.docs[0].NormalizedURL != target {
		t.Fatalf("restart replay = %t/%#v", replayNaked, index.docs)
	}
	if consumer.updateInboundAnchors(t.Context(), []IngestDelivery{delivery}) ||
		len(index.docs) != 1 {
		t.Fatalf("finalized replay reindexed %#v", index.docs)
	}
}

func TestInboundAnchorIndexLeaseBlocksConcurrentTargetDelete(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	directory, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	target := "https://target.example/page"
	if _, err := receiver.Receive(
		t.Context(),
		[]documentstore.Document{{NormalizedURL: target}},
	); err != nil {
		t.Fatal(err)
	}
	index := &blockingAnchorIndex{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	consumer := &IngestConsumer{
		anchors:  receiver.(documentstore.InboundAnchorReceiver),
		index:    index,
		observer: noopIngestObserver{},
	}
	batch := anchorUpdateBatch("https://source.example/page", target, "protected")
	updateDone := make(chan bool, 1)
	go func() {
		var naked bool
		updateDone <- consumer.updateInboundAnchors(
			t.Context(),
			[]IngestDelivery{anchorDelivery(batch, &naked)},
		)
	}()
	<-index.entered
	deleteDone := make(chan struct {
		removed bool
		err     error
	}, 1)
	go func() {
		removed, err := directory.(documentstore.DocumentEvictor).Delete(
			t.Context(),
			target,
		)
		deleteDone <- struct {
			removed bool
			err     error
		}{removed: removed, err: err}
	}()
	select {
	case outcome := <-deleteDone:
		t.Fatalf("target delete crossed index lease: %#v", outcome)
	case <-time.After(25 * time.Millisecond):
	}
	close(index.release)
	if deferred := <-updateDone; deferred {
		t.Fatal("protected anchor update was deferred")
	}
	select {
	case outcome := <-deleteDone:
		if outcome.err != nil || !outcome.removed {
			t.Fatalf("released target delete = %#v", outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("target delete remained blocked after finalization")
	}
}

func TestInboundAnchorIndexLeaseSerializesSharedTargetProjection(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	_, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	target := "https://target.example/page"
	if _, err := receiver.Receive(
		t.Context(),
		[]documentstore.Document{{NormalizedURL: target}},
	); err != nil {
		t.Fatal(err)
	}
	firstIndex := &blockingAnchorIndex{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	secondIndex := &channelAnchorIndex{
		documents: make(chan documentstore.Document, 1),
	}
	first := &IngestConsumer{
		anchors:  receiver.(documentstore.InboundAnchorReceiver),
		index:    firstIndex,
		observer: noopIngestObserver{},
	}
	second := &IngestConsumer{
		anchors:  receiver.(documentstore.InboundAnchorReceiver),
		index:    secondIndex,
		observer: noopIngestObserver{},
	}
	firstDone := make(chan bool, 1)
	go func() {
		var naked bool
		firstDone <- first.updateInboundAnchors(
			t.Context(),
			[]IngestDelivery{anchorDelivery(
				anchorUpdateBatch("https://source.example/first", target, "first"),
				&naked,
			)},
		)
	}()
	<-firstIndex.entered
	secondDone := make(chan bool, 1)
	go func() {
		var naked bool
		secondDone <- second.updateInboundAnchors(
			t.Context(),
			[]IngestDelivery{anchorDelivery(
				anchorUpdateBatch("https://source.example/second", target, "second"),
				&naked,
			)},
		)
	}()
	select {
	case doc := <-secondIndex.documents:
		t.Fatalf("shared target indexed before first finalization: %#v", doc)
	case <-time.After(25 * time.Millisecond):
	}
	close(firstIndex.release)
	if deferred := <-firstDone; deferred {
		t.Fatal("first shared-target update was deferred")
	}
	var projected documentstore.Document
	select {
	case projected = <-secondIndex.documents:
	case <-time.After(time.Second):
		t.Fatal("second shared-target projection remained blocked")
	}
	if deferred := <-secondDone; deferred {
		t.Fatal("second shared-target update was deferred")
	}
	if len(projected.Inlinks) != 2 {
		t.Fatalf("serialized target projection = %#v", projected.Inlinks)
	}
}

func anchorUpdateBatch(
	source string,
	target string,
	text string,
) yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{Document: yagocrawlcontract.DocumentIngest{
		NormalizedURL:               source,
		OutboundAnchorEvidenceKnown: true,
		OutboundAnchors: []yagocrawlcontract.OutboundAnchor{{
			TargetURL: target,
			Text:      text,
		}},
	}}
}

func TestClearOutboundAnchorsHandlesCapabilityAndSource(t *testing.T) {
	var naked bool
	delivery := anchorDelivery(yagocrawlcontract.IngestBatch{}, &naked)
	consumer := &IngestConsumer{observer: noopIngestObserver{}}
	if consumer.clearOutboundAnchors(t.Context(), delivery) {
		t.Fatal("missing capability should continue")
	}
	consumer.anchors = &anchorUpdateScript{}
	if consumer.clearOutboundAnchors(t.Context(), delivery) {
		t.Fatal("missing source should continue")
	}
	delivery.Batch.SourceURL = "https://source.example/"
	if consumer.clearOutboundAnchors(t.Context(), delivery) {
		t.Fatal("successful clear should continue")
	}
	script := consumer.anchors.(*anchorUpdateScript)
	if len(script.sets) != 1 || script.sets[0].SourceURL != delivery.Batch.SourceURL ||
		len(script.sets[0].Anchors) != 0 {
		t.Fatalf("clear sets = %#v", script.sets)
	}
}
