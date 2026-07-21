package crawlresults

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type outboundAnchorChunkScript struct {
	groups [][]documentstore.OutboundAnchorSet
}

func (s *outboundAnchorChunkScript) ReplaceOutboundAnchors(
	_ context.Context,
	sets []documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	s.groups = append(s.groups, append([]documentstore.OutboundAnchorSet(nil), sets...))

	return documentstore.AnchorUpdate{}, nil
}

func (*outboundAnchorChunkScript) VisitOutboundAnchorDocuments(
	context.Context,
	[]documentstore.OutboundAnchorFinalization,
	func([]documentstore.Document) error,
) error {
	return nil
}

func (*outboundAnchorChunkScript) FinalizeOutboundAnchors(
	context.Context,
	[]documentstore.OutboundAnchorFinalization,
) error {
	return nil
}

func (*outboundAnchorChunkScript) ReleaseOutboundAnchors(
	[]documentstore.OutboundAnchorFinalization,
) {
}

func TestUpdateInboundAnchorsChunksSixtyFourSources(t *testing.T) {
	script := &outboundAnchorChunkScript{}
	consumer := &IngestConsumer{anchors: script, observer: noopIngestObserver{}}
	deliveries, sources := outboundAnchorChunkDeliveries(64, "https://target.example/", nil, nil)

	if consumer.updateInboundAnchors(t.Context(), deliveries) {
		t.Fatal("successful anchor chunks were deferred")
	}
	if len(script.groups) != 4 {
		t.Fatalf("anchor update groups = %d, want 4", len(script.groups))
	}
	seen := make(map[string]struct{}, len(sources))
	for index, group := range script.groups {
		if len(group) != documentstore.MaximumOutboundAnchorSourcesPerReplacement {
			t.Fatalf(
				"anchor update group %d sources = %d, want %d",
				index,
				len(group),
				documentstore.MaximumOutboundAnchorSourcesPerReplacement,
			)
		}
		for _, set := range group {
			seen[set.SourceURL] = struct{}{}
		}
	}
	if len(seen) != len(sources) {
		t.Fatalf("distinct updated sources = %d, want %d", len(seen), len(sources))
	}
}

type outboundAnchorDocumentReplacementScript struct {
	outboundAnchorChunkScript
	groups        [][]documentstore.OutboundAnchorSet
	reservedCalls int
	documents     []documentstore.Document
	receipt       documentstore.AnchorReplacementReceipt
	err           error
}

func (s *outboundAnchorDocumentReplacementScript) ReplaceReservedOutboundAnchors(
	context.Context,
	documentstore.DocumentLineageReservation,
	[]documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	s.reservedCalls++

	return documentstore.AnchorUpdate{}, nil
}

func (s *outboundAnchorDocumentReplacementScript) ReplaceOutboundAnchorDocuments(
	_ context.Context,
	sets []documentstore.OutboundAnchorSet,
	visit func([]documentstore.Document) error,
) (documentstore.AnchorReplacementReceipt, error) {
	return s.replaceDocuments(sets, visit)
}

func (s *outboundAnchorDocumentReplacementScript) ReplaceReservedOutboundAnchorDocuments(
	_ context.Context,
	_ documentstore.DocumentLineageReservation,
	sets []documentstore.OutboundAnchorSet,
	visit func([]documentstore.Document) error,
) (documentstore.AnchorReplacementReceipt, error) {
	return s.replaceDocuments(sets, visit)
}

func (s *outboundAnchorDocumentReplacementScript) replaceDocuments(
	sets []documentstore.OutboundAnchorSet,
	visit func([]documentstore.Document) error,
) (documentstore.AnchorReplacementReceipt, error) {
	s.groups = append(s.groups, append([]documentstore.OutboundAnchorSet(nil), sets...))
	if s.err != nil {
		return documentstore.AnchorReplacementReceipt{}, s.err
	}
	if len(s.documents) > 0 {
		if err := visit(s.documents); err != nil {
			return documentstore.AnchorReplacementReceipt{}, err
		}
	}

	return s.receipt, nil
}

func TestReservedOutboundAnchorDocumentReplacementChunksSixtyFourSources(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	_, documents, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	deliveries, sources := outboundAnchorChunkDeliveries(64, "https://target.example/", nil, nil)
	lineages := documents.(documentstore.DocumentLineageReserver)
	reservation, err := lineages.ReserveDocumentLineages(t.Context(), sources)
	if err != nil {
		t.Fatal(err)
	}
	defer lineages.ReleaseDocumentLineages(reservation)
	script := &outboundAnchorDocumentReplacementScript{documents: []documentstore.Document{{
		NormalizedURL: "https://target.example/",
	}}}
	index := &anchorIndexScript{}
	consumer := &IngestConsumer{
		anchors:         script,
		reservedAnchors: script,
		index:           index,
		observer:        noopIngestObserver{},
	}

	if consumer.updateReservedInboundAnchors(t.Context(), deliveries, reservation) {
		t.Fatal("successful document replacement chunks were deferred")
	}
	if len(script.groups) != 4 || script.reservedCalls != 0 ||
		len(script.outboundAnchorChunkScript.groups) != 0 {
		t.Fatalf(
			"document/staged replacement calls = %d/%d/%d, want 4/0/0",
			len(script.groups),
			script.reservedCalls,
			len(script.outboundAnchorChunkScript.groups),
		)
	}
	if len(index.docs) != 4 {
		t.Fatalf("projected index documents = %d, want 4", len(index.docs))
	}
}

func TestOutboundAnchorDocumentReplacementRedeliversFailures(t *testing.T) {
	deliveryBatch := anchorUpdateBatch(
		"https://source.example/",
		"https://target.example/",
		"anchor",
	)
	tests := map[string]struct {
		receipt   documentstore.AnchorReplacementReceipt
		storeErr  error
		indexErr  error
		documents []documentstore.Document
	}{
		"busy":  {receipt: documentstore.AnchorReplacementReceipt{Busy: true}},
		"store": {storeErr: errors.New("replacement failed")},
		"index": {
			indexErr: errors.New("index failed"),
			documents: []documentstore.Document{{
				NormalizedURL: "https://target.example/",
			}},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			script := &outboundAnchorDocumentReplacementScript{
				documents: test.documents,
				receipt:   test.receipt,
				err:       test.storeErr,
			}
			consumer := &IngestConsumer{
				anchors:  script,
				index:    &anchorIndexScript{err: test.indexErr},
				observer: noopIngestObserver{},
			}
			naked := false
			if !consumer.updateInboundAnchors(
				t.Context(),
				[]IngestDelivery{anchorDelivery(deliveryBatch, &naked)},
			) || !naked {
				t.Fatal("document replacement failure did not redeliver")
			}
			if len(script.groups) != 1 || len(script.outboundAnchorChunkScript.groups) != 0 {
				t.Fatalf(
					"document/staged replacement calls = %d/%d, want 1/0",
					len(script.groups),
					len(script.outboundAnchorChunkScript.groups),
				)
			}
		})
	}
}

type failingOutboundAnchorChunkReceiver struct {
	anchors                  documentstore.InboundAnchorReceiver
	reserved                 documentstore.ReservedOutboundAnchorReceiver
	documentReplacer         documentstore.OutboundAnchorDocumentReplacer
	reservedDocumentReplacer documentstore.ReservedOutboundAnchorDocumentReplacer
	failAt                   int
	calls                    int
	groupSizes               []int
	projectionCalls          int
}

func (r *failingOutboundAnchorChunkReceiver) ReplaceOutboundAnchors(
	ctx context.Context,
	sets []documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	return r.replace(sets, func() (documentstore.AnchorUpdate, error) {
		return r.anchors.ReplaceOutboundAnchors(ctx, sets)
	})
}

func (r *failingOutboundAnchorChunkReceiver) ReplaceReservedOutboundAnchors(
	ctx context.Context,
	reservation documentstore.DocumentLineageReservation,
	sets []documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	return r.replace(sets, func() (documentstore.AnchorUpdate, error) {
		return r.reserved.ReplaceReservedOutboundAnchors(ctx, reservation, sets)
	})
}

func (r *failingOutboundAnchorChunkReceiver) replace(
	sets []documentstore.OutboundAnchorSet,
	replace func() (documentstore.AnchorUpdate, error),
) (documentstore.AnchorUpdate, error) {
	r.calls++
	r.groupSizes = append(r.groupSizes, len(sets))
	if r.calls == r.failAt {
		return documentstore.AnchorUpdate{}, errors.New("later anchor chunk failed")
	}

	return replace()
}

func (r *failingOutboundAnchorChunkReceiver) ReplaceOutboundAnchorDocuments(
	ctx context.Context,
	sets []documentstore.OutboundAnchorSet,
	visit func([]documentstore.Document) error,
) (documentstore.AnchorReplacementReceipt, error) {
	return r.replaceDocuments(sets, func() (documentstore.AnchorReplacementReceipt, error) {
		return r.documentReplacer.ReplaceOutboundAnchorDocuments(ctx, sets, r.recordVisit(visit))
	})
}

func (r *failingOutboundAnchorChunkReceiver) ReplaceReservedOutboundAnchorDocuments(
	ctx context.Context,
	reservation documentstore.DocumentLineageReservation,
	sets []documentstore.OutboundAnchorSet,
	visit func([]documentstore.Document) error,
) (documentstore.AnchorReplacementReceipt, error) {
	return r.replaceDocuments(sets, func() (documentstore.AnchorReplacementReceipt, error) {
		return r.reservedDocumentReplacer.ReplaceReservedOutboundAnchorDocuments(
			ctx,
			reservation,
			sets,
			r.recordVisit(visit),
		)
	})
}

func (r *failingOutboundAnchorChunkReceiver) replaceDocuments(
	sets []documentstore.OutboundAnchorSet,
	replace func() (documentstore.AnchorReplacementReceipt, error),
) (documentstore.AnchorReplacementReceipt, error) {
	r.calls++
	r.groupSizes = append(r.groupSizes, len(sets))
	if r.calls == r.failAt {
		return documentstore.AnchorReplacementReceipt{}, errors.New("later anchor chunk failed")
	}

	return replace()
}

func (r *failingOutboundAnchorChunkReceiver) recordVisit(
	visit func([]documentstore.Document) error,
) func([]documentstore.Document) error {
	return func(documents []documentstore.Document) error {
		r.projectionCalls++

		return visit(documents)
	}
}

func (r *failingOutboundAnchorChunkReceiver) VisitOutboundAnchorDocuments(
	ctx context.Context,
	finalizations []documentstore.OutboundAnchorFinalization,
	visit func([]documentstore.Document) error,
) error {
	if err := r.anchors.VisitOutboundAnchorDocuments(ctx, finalizations, visit); err != nil {
		return fmt.Errorf("visit outbound anchor documents: %w", err)
	}

	return nil
}

func (r *failingOutboundAnchorChunkReceiver) FinalizeOutboundAnchors(
	ctx context.Context,
	finalizations []documentstore.OutboundAnchorFinalization,
) error {
	if err := r.anchors.FinalizeOutboundAnchors(ctx, finalizations); err != nil {
		return fmt.Errorf("finalize outbound anchors: %w", err)
	}

	return nil
}

func (r *failingOutboundAnchorChunkReceiver) ReleaseOutboundAnchors(
	finalizations []documentstore.OutboundAnchorFinalization,
) {
	r.anchors.ReleaseOutboundAnchors(finalizations)
}

type outboundAnchorChunkURLReceiver struct {
	calls int
}

func (r *outboundAnchorChunkURLReceiver) Receive(
	context.Context,
	[]yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	r.calls++

	return urlmeta.Receipt{}, nil
}

type outboundAnchorChunkPostingReceiver struct {
	calls int
}

func (r *outboundAnchorChunkPostingReceiver) Receive(
	context.Context,
	[]yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	r.calls++

	return rwi.Receipt{}, nil
}

type outboundAnchorChunkReplayEvidence struct {
	directory  documentstore.DocumentDirectory
	target     string
	wrapper    *failingOutboundAnchorChunkReceiver
	urls       *outboundAnchorChunkURLReceiver
	postings   *outboundAnchorChunkPostingReceiver
	acked      *int
	naked      *int
	deliveries []IngestDelivery
	sources    []string
}

func TestLaterOutboundAnchorChunkFailureRedeliversAndReplaysSixtyFourSources(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	directory, documents, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	target := "https://target.example/page"
	if _, err := documents.Receive(
		t.Context(),
		[]documentstore.Document{{NormalizedURL: target}},
	); err != nil {
		t.Fatal(err)
	}
	anchors := documents.(documentstore.InboundAnchorReceiver)
	lineages := documents.(documentstore.DocumentLineageReserver)
	reserved := documents.(documentstore.ReservedOutboundAnchorReceiver)
	wrapper := &failingOutboundAnchorChunkReceiver{
		anchors:                  anchors,
		reserved:                 reserved,
		documentReplacer:         documents.(documentstore.OutboundAnchorDocumentReplacer),
		reservedDocumentReplacer: documents.(documentstore.ReservedOutboundAnchorDocumentReplacer),
		failAt:                   3,
	}
	urls := &outboundAnchorChunkURLReceiver{}
	postings := &outboundAnchorChunkPostingReceiver{}
	consumer := NewIngestConsumer(nil, nil, urls, postings)
	consumer.anchors = wrapper
	consumer.reservedAnchors = wrapper
	acked := 0
	naked := 0
	deliveries, sources := outboundAnchorChunkDeliveries(64, target, &acked, &naked)
	evidence := outboundAnchorChunkReplayEvidence{
		directory:  directory,
		target:     target,
		wrapper:    wrapper,
		urls:       urls,
		postings:   postings,
		acked:      &acked,
		naked:      &naked,
		deliveries: deliveries,
		sources:    sources,
	}

	reservation, err := lineages.ReserveDocumentLineages(t.Context(), sources)
	if err != nil {
		t.Fatal(err)
	}
	consumer.absorbReservedTailGroup(t.Context(), deliveries, reservation)
	lineages.ReleaseDocumentLineages(reservation)
	requirePartialOutboundAnchorChunkAttempt(t, evidence)

	reservation, err = lineages.ReserveDocumentLineages(t.Context(), sources)
	if err != nil {
		t.Fatal(err)
	}
	consumer.absorbReservedTailGroup(t.Context(), deliveries, reservation)
	lineages.ReleaseDocumentLineages(reservation)
	requireCompletedOutboundAnchorChunkReplay(t, evidence)
}

func requirePartialOutboundAnchorChunkAttempt(
	t *testing.T,
	evidence outboundAnchorChunkReplayEvidence,
) {
	t.Helper()
	if *evidence.acked != 0 || *evidence.naked != len(evidence.deliveries) {
		t.Fatalf(
			"first attempt acks/naks = %d/%d, want 0/%d",
			*evidence.acked,
			*evidence.naked,
			len(evidence.deliveries),
		)
	}
	if evidence.urls.calls != 0 || evidence.postings.calls != 0 {
		t.Fatalf(
			"tail stores after chunk failure = %d/%d, want 0/0",
			evidence.urls.calls,
			evidence.postings.calls,
		)
	}
	if len(evidence.wrapper.groupSizes) != 3 {
		t.Fatalf(
			"first attempt anchor groups = %v, want three attempted chunks",
			evidence.wrapper.groupSizes,
		)
	}
	stored, found, err := evidence.directory.Document(t.Context(), evidence.target)
	if err != nil || !found || len(stored.Inlinks) != 32 {
		t.Fatalf(
			"partially committed target = %t/%d/%v, want true/32/nil",
			found,
			len(stored.Inlinks),
			err,
		)
	}
}

func requireCompletedOutboundAnchorChunkReplay(
	t *testing.T,
	evidence outboundAnchorChunkReplayEvidence,
) {
	t.Helper()
	if *evidence.acked != len(evidence.deliveries) || *evidence.naked != len(evidence.deliveries) {
		t.Fatalf(
			"replay acks/naks = %d/%d, want %d/%d",
			*evidence.acked,
			*evidence.naked,
			len(evidence.deliveries),
			len(evidence.deliveries),
		)
	}
	if evidence.urls.calls != 1 || evidence.postings.calls != 1 {
		t.Fatalf(
			"replay tail stores = %d/%d, want 1/1",
			evidence.urls.calls,
			evidence.postings.calls,
		)
	}
	if len(evidence.wrapper.groupSizes) != 7 {
		t.Fatalf(
			"all attempted anchor groups = %v, want three plus four",
			evidence.wrapper.groupSizes,
		)
	}
	for index, size := range evidence.wrapper.groupSizes {
		if size != documentstore.MaximumOutboundAnchorSourcesPerReplacement {
			t.Fatalf(
				"anchor group %d sources = %d, want %d",
				index,
				size,
				documentstore.MaximumOutboundAnchorSourcesPerReplacement,
			)
		}
	}
	if evidence.wrapper.projectionCalls != 4 {
		t.Fatalf("anchor projection calls = %d, want 4", evidence.wrapper.projectionCalls)
	}
	stored, found, err := evidence.directory.Document(t.Context(), evidence.target)
	if err != nil || !found {
		t.Fatalf("target document found/error = %t/%v", found, err)
	}
	if len(stored.Inlinks) != len(evidence.sources) {
		t.Fatalf("target inlinks = %d, want %d", len(stored.Inlinks), len(evidence.sources))
	}
	linkedSources := make(map[string]struct{}, len(stored.Inlinks))
	for _, anchor := range stored.Inlinks {
		linkedSources[anchor.URL] = struct{}{}
	}
	if len(linkedSources) != len(evidence.sources) {
		t.Fatalf("distinct target inlinks = %d, want %d", len(linkedSources), len(evidence.sources))
	}
}

func outboundAnchorChunkDeliveries(
	total int,
	target string,
	acked *int,
	naked *int,
) ([]IngestDelivery, []string) {
	deliveries := make([]IngestDelivery, 0, total)
	sources := make([]string, 0, total)
	for index := range total {
		source := fmt.Sprintf("https://source.example/%02d", index)
		sources = append(sources, source)
		delivery := IngestDelivery{Batch: anchorUpdateBatch(source, target, "anchor")}
		delivery.Batch.SourceURL = source
		if acked != nil {
			delivery.Ack = func(context.Context) error {
				(*acked)++

				return nil
			}
		}
		if naked != nil {
			delivery.Nak = func(context.Context) error {
				(*naked)++

				return nil
			}
		}
		deliveries = append(deliveries, delivery)
	}

	return deliveries, sources
}
