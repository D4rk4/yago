package yagonode

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type documentLineageTestStream struct {
	deliveries chan crawlresults.IngestDelivery
}

func (s documentLineageTestStream) Receive() <-chan crawlresults.IngestDelivery {
	return s.deliveries
}

type documentLineageTestURLReceiver struct{}

func (documentLineageTestURLReceiver) Receive(
	context.Context,
	[]yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	return urlmeta.Receipt{}, nil
}

type documentLineageTestPostingReceiver struct{}

func (documentLineageTestPostingReceiver) Receive(
	context.Context,
	[]yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	return rwi.Receipt{}, nil
}

type observedDocumentLineages struct {
	inner   documentstore.DocumentLineageReserver
	entered chan struct{}
	once    sync.Once
}

func (o *observedDocumentLineages) ReserveDocumentLineages(
	ctx context.Context,
	urls []string,
) (documentstore.DocumentLineageReservation, error) {
	o.once.Do(func() { close(o.entered) })

	reservation, err := o.inner.ReserveDocumentLineages(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("reserve observed document lineages: %w", err)
	}

	return reservation, nil
}

func (o *observedDocumentLineages) ReleaseDocumentLineages(
	reservation documentstore.DocumentLineageReservation,
) {
	o.inner.ReleaseDocumentLineages(reservation)
}

type pausedDocumentLineageReceiver struct {
	receiver          documentstore.DocumentReceiver
	anchors           documentstore.InboundAnchorReceiver
	lineages          documentstore.DocumentLineageReserver
	reservedAnchors   documentstore.ReservedOutboundAnchorReceiver
	reservedDocuments documentstore.ReservedCanonicalDocumentDirectory
	anchorEntered     chan struct{}
	anchorProceed     chan struct{}
	reserveEntered    chan struct{}
	anchorOnce        sync.Once
	reserveOnce       sync.Once
}

func (p *pausedDocumentLineageReceiver) Receive(
	ctx context.Context,
	documents []documentstore.Document,
) (documentstore.Receipt, error) {
	receipt, err := p.receiver.Receive(ctx, documents)
	if err != nil {
		return documentstore.Receipt{}, fmt.Errorf("receive paused document lineages: %w", err)
	}

	return receipt, nil
}

func (p *pausedDocumentLineageReceiver) ReplaceOutboundAnchors(
	ctx context.Context,
	sets []documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	update, err := p.anchors.ReplaceOutboundAnchors(ctx, sets)
	if err != nil {
		return documentstore.AnchorUpdate{}, fmt.Errorf("replace paused outbound anchors: %w", err)
	}

	return update, nil
}

func (p *pausedDocumentLineageReceiver) VisitOutboundAnchorDocuments(
	ctx context.Context,
	finalizations []documentstore.OutboundAnchorFinalization,
	visit func([]documentstore.Document) error,
) error {
	if err := p.anchors.VisitOutboundAnchorDocuments(ctx, finalizations, visit); err != nil {
		return fmt.Errorf("visit paused outbound anchor documents: %w", err)
	}

	return nil
}

func (p *pausedDocumentLineageReceiver) FinalizeOutboundAnchors(
	ctx context.Context,
	finalizations []documentstore.OutboundAnchorFinalization,
) error {
	if err := p.anchors.FinalizeOutboundAnchors(ctx, finalizations); err != nil {
		return fmt.Errorf("finalize paused outbound anchors: %w", err)
	}

	return nil
}

func (p *pausedDocumentLineageReceiver) ReleaseOutboundAnchors(
	finalizations []documentstore.OutboundAnchorFinalization,
) {
	p.anchors.ReleaseOutboundAnchors(finalizations)
}

func (p *pausedDocumentLineageReceiver) ReserveDocumentLineages(
	ctx context.Context,
	urls []string,
) (documentstore.DocumentLineageReservation, error) {
	if p.reserveEntered != nil {
		p.reserveOnce.Do(func() { close(p.reserveEntered) })
	}

	reservation, err := p.lineages.ReserveDocumentLineages(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("reserve paused document lineages: %w", err)
	}

	return reservation, nil
}

func (p *pausedDocumentLineageReceiver) ReleaseDocumentLineages(
	reservation documentstore.DocumentLineageReservation,
) {
	p.lineages.ReleaseDocumentLineages(reservation)
}

func (p *pausedDocumentLineageReceiver) ReplaceReservedOutboundAnchors(
	ctx context.Context,
	reservation documentstore.DocumentLineageReservation,
	sets []documentstore.OutboundAnchorSet,
) (documentstore.AnchorUpdate, error) {
	if p.anchorEntered != nil {
		p.anchorOnce.Do(func() { close(p.anchorEntered) })
	}
	if p.anchorProceed != nil {
		select {
		case <-p.anchorProceed:
		case <-ctx.Done():
			return documentstore.AnchorUpdate{}, fmt.Errorf("pause outbound anchors: %w", ctx.Err())
		}
	}

	update, err := p.reservedAnchors.ReplaceReservedOutboundAnchors(ctx, reservation, sets)
	if err != nil {
		return documentstore.AnchorUpdate{}, fmt.Errorf("replace paused reserved anchors: %w", err)
	}

	return update, nil
}

func (p *pausedDocumentLineageReceiver) CanonicalReservedDocuments(
	ctx context.Context,
	reservation documentstore.DocumentLineageReservation,
	documents []documentstore.Document,
) ([]documentstore.Document, error) {
	canonical, err := p.reservedDocuments.CanonicalReservedDocuments(
		ctx,
		reservation,
		documents,
	)
	if err != nil {
		return nil, fmt.Errorf("canonicalize paused reserved documents: %w", err)
	}

	return canonical, nil
}

func TestIngestThenEvictionLeavesNoGhostDocumentLineage(t *testing.T) {
	directory, receiver := openDocumentLineageSerializationStore(t)
	sourceURL := "https://source.example/"
	targetURL := "https://target.example/"
	seedDocumentLineageTarget(t, receiver, targetURL)
	anchorEntered := make(chan struct{})
	anchorProceed := make(chan struct{})
	wrapped := newPausedDocumentLineageReceiver(receiver)
	wrapped.anchorEntered = anchorEntered
	wrapped.anchorProceed = anchorProceed
	stream := documentLineageTestStream{deliveries: make(chan crawlresults.IngestDelivery, 1)}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		wrapped,
		documentLineageTestURLReceiver{},
		documentLineageTestPostingReceiver{},
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go consumer.Run(ctx)
	acked := make(chan struct{})
	stream.deliveries <- documentLineageDelivery(sourceURL, targetURL, acked, t)
	waitDocumentLineageSignal(t, anchorEntered)

	evictionEntered := make(chan struct{})
	lineages := &observedDocumentLineages{
		inner:   receiver.(documentstore.DocumentLineageReserver),
		entered: evictionEntered,
	}
	evictor := documentLineageEvictor{
		directory:       directory,
		receiver:        receiver,
		documents:       directory.(documentstore.DocumentEvictor),
		anchors:         receiver.(documentstore.InboundAnchorReceiver),
		lineages:        lineages,
		reservedAnchors: receiver.(documentstore.ReservedOutboundAnchorReceiver),
	}
	type evictionResult struct {
		removed bool
		err     error
	}
	evicted := make(chan evictionResult, 1)
	go func() {
		removed, err := evictor.Delete(ctx, sourceURL)
		evicted <- evictionResult{removed: removed, err: err}
	}()
	waitDocumentLineageSignal(t, evictionEntered)
	assertDocumentLineagePending(t, evicted)
	close(anchorProceed)
	waitDocumentLineageSignal(t, acked)
	result := waitDocumentLineageResult(t, evicted)
	if result.err != nil || !result.removed {
		t.Fatalf("eviction result = %t, %v", result.removed, result.err)
	}
	assertDocumentLineageState(
		t,
		directory,
		sourceURL,
		targetURL,
		documentLineageExpectedState{},
	)
}

func TestEvictionThenIngestPublishesOneCurrentDocumentLineage(t *testing.T) {
	directory, receiver := openDocumentLineageSerializationStore(t)
	sourceURL := "https://source.example/"
	targetURL := "https://target.example/"
	seedDocumentLineageTarget(t, receiver, targetURL)
	evictor := documentLineageEvictor{
		directory:       directory,
		receiver:        receiver,
		documents:       directory.(documentstore.DocumentEvictor),
		anchors:         receiver.(documentstore.InboundAnchorReceiver),
		lineages:        receiver.(documentstore.DocumentLineageReserver),
		reservedAnchors: receiver.(documentstore.ReservedOutboundAnchorReceiver),
	}
	reservation, err := evictor.ReserveDocumentEvictions(t.Context(), []string{sourceURL})
	if err != nil {
		t.Fatalf("reserve eviction: %v", err)
	}
	defer reservation.Release()
	reserveEntered := make(chan struct{})
	wrapped := newPausedDocumentLineageReceiver(receiver)
	wrapped.reserveEntered = reserveEntered
	stream := documentLineageTestStream{deliveries: make(chan crawlresults.IngestDelivery, 1)}
	consumer := crawlresults.NewIngestConsumer(
		stream,
		wrapped,
		documentLineageTestURLReceiver{},
		documentLineageTestPostingReceiver{},
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go consumer.Run(ctx)
	acked := make(chan struct{})
	stream.deliveries <- documentLineageDelivery(sourceURL, targetURL, acked, t)
	waitDocumentLineageSignal(t, reserveEntered)
	assertDocumentLineagePending(t, acked)
	removed, err := reservation.Delete(t.Context(), sourceURL)
	if err != nil || removed {
		t.Fatalf("reserved eviction = %t, %v", removed, err)
	}
	reservation.Release()
	waitDocumentLineageSignal(t, acked)
	assertDocumentLineageState(
		t,
		directory,
		sourceURL,
		targetURL,
		documentLineageExpectedState{sourcePresent: true, anchorPresent: true},
	)
}

func openDocumentLineageSerializationStore(
	t *testing.T,
) (documentstore.DocumentDirectory, documentstore.DocumentReceiver) {
	t.Helper()
	directory, receiver, err := documentstore.Open(openTestVault(t))
	if err != nil {
		t.Fatalf("open documents: %v", err)
	}

	return directory, receiver
}

func newPausedDocumentLineageReceiver(
	receiver documentstore.DocumentReceiver,
) *pausedDocumentLineageReceiver {
	return &pausedDocumentLineageReceiver{
		receiver:          receiver,
		anchors:           receiver.(documentstore.InboundAnchorReceiver),
		lineages:          receiver.(documentstore.DocumentLineageReserver),
		reservedAnchors:   receiver.(documentstore.ReservedOutboundAnchorReceiver),
		reservedDocuments: receiver.(documentstore.ReservedCanonicalDocumentDirectory),
	}
}

func seedDocumentLineageTarget(
	t *testing.T,
	receiver documentstore.DocumentReceiver,
	targetURL string,
) {
	t.Helper()
	if _, err := receiver.Receive(t.Context(), []documentstore.Document{{
		NormalizedURL: targetURL,
		CanonicalURL:  targetURL,
	}}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
}

func documentLineageDelivery(
	sourceURL string,
	targetURL string,
	acked chan struct{},
	t *testing.T,
) crawlresults.IngestDelivery {
	return crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: sourceURL,
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL:               sourceURL,
				CanonicalURL:                sourceURL,
				ExtractedText:               "alpha beta gamma delta",
				OutboundAnchorEvidenceKnown: true,
				OutboundAnchors: []yagocrawlcontract.OutboundAnchor{{
					TargetURL: targetURL,
					Text:      "source evidence",
				}},
			},
		},
		Ack: func(context.Context) error {
			close(acked)

			return nil
		},
		Nak: func(context.Context) error {
			t.Error("unexpected ingest redelivery")

			return nil
		},
	}
}

func waitDocumentLineageSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("document lineage operation timed out")
	}
}

func assertDocumentLineagePending[T any](t *testing.T, result <-chan T) {
	t.Helper()
	select {
	case <-result:
		t.Fatal("document lineage operation completed before reservation release")
	default:
	}
}

func waitDocumentLineageResult[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("document lineage result timed out")
		var zero T

		return zero
	}
}

type documentLineageExpectedState struct {
	sourcePresent bool
	anchorPresent bool
}

func assertDocumentLineageState(
	t *testing.T,
	directory documentstore.DocumentDirectory,
	sourceURL string,
	targetURL string,
	expected documentLineageExpectedState,
) {
	t.Helper()
	_, found, err := directory.Document(t.Context(), sourceURL)
	if err != nil || found != expected.sourcePresent {
		t.Fatalf("source document = %t, %v", found, err)
	}
	target, found, err := directory.Document(t.Context(), targetURL)
	if err != nil || !found {
		t.Fatalf("target document = %t, %v", found, err)
	}
	hasSourceAnchor := false
	for _, anchor := range target.Inlinks {
		if anchor.URL == sourceURL {
			hasSourceAnchor = true
		}
	}
	if hasSourceAnchor != expected.anchorPresent {
		t.Fatalf("source anchor = %t, want %t", hasSourceAnchor, expected.anchorPresent)
	}
}

var _ eviction.ReservedDocumentEviction = (*reservedDocumentLineageEviction)(nil)
