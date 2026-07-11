package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type anchorUpdateScript struct {
	update documentstore.AnchorUpdate
	err    error
	sets   []documentstore.OutboundAnchorSet
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
	script.update.Documents = []documentstore.Document{target}
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
		update   documentstore.AnchorUpdate
		storeErr error
		indexErr error
	}{
		"store error": {storeErr: errors.New("store failed")},
		"busy":        {update: documentstore.AnchorUpdate{Busy: true}},
		"index error": {
			update: documentstore.AnchorUpdate{Documents: []documentstore.Document{{
				NormalizedURL: "https://target.example/",
			}}},
			indexErr: errors.New("index failed"),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var naked bool
			script := &anchorUpdateScript{update: test.update, err: test.storeErr}
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
