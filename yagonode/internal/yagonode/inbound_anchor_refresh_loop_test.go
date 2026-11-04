package yagonode

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type pagedInboundAnchorRefresher struct {
	receipts []documentstore.InboundAnchorRefreshReceipt
	err      error
	calls    int
	limits   []int
	signal   chan struct{}
}

func (p *pagedInboundAnchorRefresher) RefreshInboundAnchorDocuments(
	_ context.Context,
	limit int,
	reindex func([]documentstore.Document) error,
) (documentstore.InboundAnchorRefreshReceipt, error) {
	p.calls++
	p.limits = append(p.limits, limit)
	if p.signal != nil {
		p.signal <- struct{}{}
	}
	if p.err != nil {
		return documentstore.InboundAnchorRefreshReceipt{}, p.err
	}
	receipt := p.receipts[0]
	p.receipts = p.receipts[1:]
	if err := reindex(make([]documentstore.Document, receipt.Refreshed)); err != nil {
		return documentstore.InboundAnchorRefreshReceipt{}, err
	}

	return receipt, nil
}

func seedPendingInboundAnchorRefresh(
	t *testing.T,
) (documentstore.DocumentDirectory, documentstore.InboundAnchorDocumentRefresher) {
	t.Helper()
	directory, receiver, err := documentstore.Open(openTestVault(t))
	if err != nil {
		t.Fatal(err)
	}
	targetURL := "https://target.example/refresh"
	if _, err := receiver.Receive(t.Context(), []documentstore.Document{{
		NormalizedURL: targetURL,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.(documentstore.OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]documentstore.OutboundAnchorSet{{
			SourceURL: "https://source.example/refresh",
			Anchors: []documentstore.OutboundAnchor{{
				TargetURL: targetURL,
				Text:      "refresh",
			}},
		}},
	); err != nil {
		t.Fatal(err)
	}

	return directory, receiver.(documentstore.InboundAnchorDocumentRefresher)
}

func TestDrainInboundAnchorRefreshesReindexesPendingTargetsOnce(t *testing.T) {
	directory, refresher := seedPendingInboundAnchorRefresh(t)
	index := &lineageBatchIndexScript{}
	refreshed, err := drainInboundAnchorRefreshes(t.Context(), refresher, index)
	if err != nil || refreshed != 1 || index.batches != 1 || len(index.docs) != 1 ||
		len(index.docs[0].Inlinks) != 1 {
		t.Fatalf("drain = %d/%v, index = %#v", refreshed, err, index)
	}
	stored, found, err := directory.Document(t.Context(), "https://target.example/refresh")
	if err != nil || !found || len(stored.Inlinks) != 1 {
		t.Fatalf("refreshed target = %#v/%t/%v", stored.Inlinks, found, err)
	}
	refreshed, err = drainInboundAnchorRefreshes(t.Context(), refresher, index)
	if err != nil || refreshed != 0 || index.batches != 1 {
		t.Fatalf("second drain = %d/%v, batches = %d", refreshed, err, index.batches)
	}
}

func TestDrainInboundAnchorRefreshesKeepsMarkWhenReindexFails(t *testing.T) {
	_, refresher := seedPendingInboundAnchorRefresh(t)
	wantErr := errors.New("index failed")
	failing := &lineageIndexScript{err: wantErr}
	_, err := drainInboundAnchorRefreshes(t.Context(), refresher, failing)
	if !errors.Is(err, wantErr) {
		t.Fatalf("single-document index failure = %v", err)
	}
	batchFailing := &lineageBatchIndexScript{lineageIndexScript: lineageIndexScript{err: wantErr}}
	_, err = drainInboundAnchorRefreshes(t.Context(), refresher, batchFailing)
	if !errors.Is(err, wantErr) {
		t.Fatalf("batch index failure = %v", err)
	}
	index := &lineageIndexScript{}
	refreshed, err := drainInboundAnchorRefreshes(t.Context(), refresher, index)
	if err != nil || refreshed != 1 || len(index.docs) != 1 {
		t.Fatalf("retry after index failure = %d/%v, index = %#v", refreshed, err, index.docs)
	}
}

func TestDrainInboundAnchorRefreshesFollowsPagesUntilDone(t *testing.T) {
	refresher := &pagedInboundAnchorRefresher{receipts: []documentstore.InboundAnchorRefreshReceipt{
		{Refreshed: 2, More: true},
		{Refreshed: 1},
	}}
	refreshed, err := drainInboundAnchorRefreshes(t.Context(), refresher, nil)
	if err != nil || refreshed != 3 || refresher.calls != 2 {
		t.Fatalf("paged drain = %d/%v after %d calls", refreshed, err, refresher.calls)
	}
	for _, limit := range refresher.limits {
		if limit != inboundAnchorRefreshPageSize {
			t.Fatalf("refresh page limit = %d", limit)
		}
	}
}

func TestRefreshInboundAnchorsOnceWarnsOnlyOnFailure(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	refreshInboundAnchorsOnce(t.Context(), &pagedInboundAnchorRefresher{
		receipts: []documentstore.InboundAnchorRefreshReceipt{{Refreshed: 1}},
	}, nil)
	if strings.Contains(output.String(), inboundAnchorRefreshFailMessage) ||
		!strings.Contains(output.String(), inboundAnchorRefreshDoneMessage) {
		t.Fatalf("successful refresh log = %s", output.String())
	}
	output.Reset()
	refreshInboundAnchorsOnce(t.Context(), &pagedInboundAnchorRefresher{
		err: errors.New("refresh failed"),
	}, nil)
	if !strings.Contains(output.String(), inboundAnchorRefreshFailMessage) ||
		!strings.Contains(output.String(), `"level":"WARN"`) {
		t.Fatalf("failed refresh log = %s", output.String())
	}
}

func TestRunInboundAnchorRefreshLoopRefreshesOnTickAndStops(t *testing.T) {
	ticks := make(chan time.Time)
	saved := newInboundAnchorRefreshTicks
	newInboundAnchorRefreshTicks = func() (<-chan time.Time, func()) { return ticks, func() {} }
	t.Cleanup(func() { newInboundAnchorRefreshTicks = saved })

	refresher := &pagedInboundAnchorRefresher{
		receipts: []documentstore.InboundAnchorRefreshReceipt{{}},
		signal:   make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		runInboundAnchorRefreshLoop(ctx, refresher, nil)
		close(done)
	}()
	ticks <- time.Now()
	select {
	case <-refresher.signal:
	case <-time.After(2 * time.Second):
		t.Fatal("tick did not trigger a refresh")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh loop did not stop on context cancel")
	}
}

func TestRunInboundAnchorRefreshLoopWithoutRefresherReturns(t *testing.T) {
	saved := newInboundAnchorRefreshTicks
	newInboundAnchorRefreshTicks = func() (<-chan time.Time, func()) {
		t.Fatal("refresh loop started a clock without a refresher")

		return nil, func() {}
	}
	t.Cleanup(func() { newInboundAnchorRefreshTicks = saved })

	runInboundAnchorRefreshLoop(t.Context(), nil, nil)
	if inboundAnchorRefresher(nodeStorage{}) != nil {
		t.Fatal("storage without a document receiver produced a refresher")
	}
}
