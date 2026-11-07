package yagonode

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

var (
	waitingPurgeWord     = yagomodel.Hash("CCCCCCCCCCCC")
	waitingPurgeURL      = yagomodel.Hash("DDDDDDDDDDDD")
	waitingPurgeKeptURL  = yagomodel.Hash("EEEEEEEEEEEE")
	waitingPurgeKeptURLs = []yagomodel.Hash{waitingPurgeKeptURL}
)

type rejectedDHTHandoff struct{}

func (rejectedDHTHandoff) Send(
	context.Context,
	yagomodel.Seed,
	[]yagomodel.RWIPosting,
) (indextransfer.HandoffReceipt, error) {
	return indextransfer.HandoffReceipt{State: indextransfer.HandoffRWIRejected}, nil
}

func selectWaitingPurgePostings(
	t *testing.T,
	ctx context.Context,
) (nodeStorage, dhtOutboundRWIWords, []yagomodel.WordPostings) {
	t.Helper()

	v := openTestVault(t)
	storage, err := openNodeStorage(v, "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	storeSenderDHTRows(t, ctx, storage, waitingPurgeWord, waitingPurgeURL)
	storeSenderDHTRows(t, ctx, storage, waitingPurgeWord, waitingPurgeKeptURL)
	source := dhtOutboundRWIWords{postings: storage.outboundPostings}
	words, err := source.SelectOutboundWords(ctx, 1, 10)
	if err != nil {
		t.Fatalf("SelectOutboundWords: %v", err)
	}
	evictor := eviction.NewEvictor(
		v,
		storage.postingPurger,
		storage.references,
		storage.urlEvictor,
		nil,
		nil,
	)
	if _, err := evictor.EvictURLs(ctx, []yagomodel.Hash{waitingPurgeURL}); err != nil {
		t.Fatalf("EvictURLs: %v", err)
	}

	return storage, source, words
}

func storedWordURLs(t *testing.T, ctx context.Context, storage nodeStorage) []yagomodel.Hash {
	t.Helper()

	var urls []yagomodel.Hash
	if err := storage.postings.ScanWord(
		ctx,
		waitingPurgeWord,
		func(posting yagomodel.RWIPosting) (bool, error) {
			url, err := posting.URLHash()
			if err != nil {
				return false, fmt.Errorf("posting url hash: %w", err)
			}
			urls = append(urls, url.Hash())

			return true, nil
		},
	); err != nil {
		t.Fatalf("ScanWord: %v", err)
	}

	return urls
}

func TestURLDeletionDropsWaitingOutboundPostingBeforeRestore(t *testing.T) {
	ctx := t.Context()
	storage, source, words := selectWaitingPurgePostings(t, ctx)

	restored, err := source.RestoreOutboundWords(ctx, words)
	if err != nil || restored != 1 {
		t.Fatalf("RestoreOutboundWords = %d, %v; want 1, nil", restored, err)
	}
	if urls := storedWordURLs(t, ctx, storage); !slices.Equal(urls, waitingPurgeKeptURLs) {
		t.Fatalf("stored urls = %v, want %v", urls, waitingPurgeKeptURLs)
	}
	recovered, err := storage.outboundPostings.RecoverOutbound(ctx)
	if err != nil || recovered != 0 {
		t.Fatalf("RecoverOutbound = %d, %v; want 0, nil", recovered, err)
	}
}

func TestURLDeletionDropsWaitingOutboundPostingBeforeRecovery(t *testing.T) {
	ctx := t.Context()
	storage, _, _ := selectWaitingPurgePostings(t, ctx)

	recovered, err := storage.outboundPostings.RecoverOutbound(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverOutbound = %d, %v; want 1, nil", recovered, err)
	}
	if urls := storedWordURLs(t, ctx, storage); !slices.Equal(urls, waitingPurgeKeptURLs) {
		t.Fatalf("stored urls = %v, want %v", urls, waitingPurgeKeptURLs)
	}
}

func distributeSingleDHTSelection(
	t *testing.T,
	ctx context.Context,
	handoff dhtexchange.IndexHandoff,
) (nodeStorage, dhtexchange.DistributionReceipt) {
	t.Helper()

	storage, err := openNodeStorage(openTestVault(t), "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	storeSenderDHTRows(t, ctx, storage, waitingPurgeWord, waitingPurgeURL)
	source := dhtOutboundRWIWords{postings: storage.outboundPostings}
	queue := dhtexchange.NewOutboundQueue()
	peer := dhtOutboundPeer(t)
	feed, err := dhtexchange.NewOutboundFeeder(
		queue,
		source,
		storage.urlDirectory,
		func(context.Context) []yagomodel.Seed { return []yagomodel.Seed{peer} },
		dhtexchange.OutboundFeederConfig{Redundancy: 1, MinimumPeerAgeDays: -1},
	).Feed(ctx)
	if err != nil || feed.State != dhtexchange.OutboundFeedEnqueued {
		t.Fatalf("Feed = %#v, %v", feed, err)
	}
	receipt, err := dhtexchange.NewConfirmingOutboundDistributor(
		queue,
		handoff,
		source,
		source,
	).DistributeReady(ctx, openDHTDurabilityGateState(), openDHTDurabilityGateConfig(), nil)
	if err != nil {
		t.Fatalf("DistributeReady: %v", err)
	}

	return storage, receipt
}

func TestDHTOutboundAdapterConfirmReleasesWaitingPosting(t *testing.T) {
	ctx := t.Context()
	storage, receipt := distributeSingleDHTSelection(t, ctx, acceptedDHTHandoff{})

	if receipt.ConfirmedPostings != 1 {
		t.Fatalf("confirmed = %d, want 1", receipt.ConfirmedPostings)
	}
	recovered, err := storage.outboundPostings.RecoverOutbound(ctx)
	if err != nil || recovered != 0 {
		t.Fatalf("RecoverOutbound = %d, %v; want 0, nil", recovered, err)
	}
	if urls := storedWordURLs(t, ctx, storage); len(urls) != 0 {
		t.Fatalf("stored urls = %v, want none", urls)
	}
}

func TestDHTOutboundAdapterRestoreReleasesWaitingPosting(t *testing.T) {
	ctx := t.Context()
	storage, receipt := distributeSingleDHTSelection(t, ctx, rejectedDHTHandoff{})

	if receipt.State != dhtexchange.DistributionHandoffRejected || receipt.RestoredPostings != 1 {
		t.Fatalf(
			"receipt state/restored = %s/%d, want %s/1",
			receipt.State,
			receipt.RestoredPostings,
			dhtexchange.DistributionHandoffRejected,
		)
	}
	recovered, err := storage.outboundPostings.RecoverOutbound(ctx)
	if err != nil || recovered != 0 {
		t.Fatalf("RecoverOutbound = %d, %v; want 0, nil", recovered, err)
	}
	want := []yagomodel.Hash{waitingPurgeURL}
	if urls := storedWordURLs(t, ctx, storage); !slices.Equal(urls, want) {
		t.Fatalf("stored urls = %v, want %v", urls, want)
	}
}
