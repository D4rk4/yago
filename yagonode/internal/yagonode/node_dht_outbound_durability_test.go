package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

type acceptedDHTHandoff struct{}

func (acceptedDHTHandoff) Send(
	context.Context,
	yagomodel.Seed,
	[]yagomodel.RWIPosting,
) (indextransfer.HandoffReceipt, error) {
	return indextransfer.HandoffReceipt{State: indextransfer.HandoffRWIOnly}, nil
}

func TestDHTOutboundJournalRecoversBeforeFinalRedundancyCopy(t *testing.T) {
	ctx := t.Context()
	storage, source, queue := preparedRedundantDHTSelection(t, ctx)
	distributor := dhtexchange.NewConfirmingOutboundDistributor(
		queue,
		acceptedDHTHandoff{},
		source,
		source,
	)

	receipt, err := distributor.Distribute(
		ctx,
		openDHTDurabilityGateState(),
		openDHTDurabilityGateConfig(),
	)
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}
	if receipt.ConfirmedPostings != 0 || queue.PostingCount() != 1 {
		t.Fatalf("receipt/queue = %#v/%d", receipt, queue.PostingCount())
	}
	recovered, err := storage.outboundPostings.RecoverOutbound(ctx)
	if err != nil {
		t.Fatalf("RecoverOutbound: %v", err)
	}
	count, countErr := storage.postings.RWICount(ctx)
	if recovered != 1 || countErr != nil || count != 1 {
		t.Fatalf("recovered/count = %d/%d, %v", recovered, count, countErr)
	}
}

func TestDHTOutboundJournalFinalizesAfterEveryRedundancyCopy(t *testing.T) {
	ctx := t.Context()
	storage, source, queue := preparedRedundantDHTSelection(t, ctx)
	distributor := dhtexchange.NewConfirmingOutboundDistributor(
		queue,
		acceptedDHTHandoff{},
		source,
		source,
	)

	first, err := distributor.Distribute(
		ctx,
		openDHTDurabilityGateState(),
		openDHTDurabilityGateConfig(),
	)
	if err != nil {
		t.Fatalf("first Distribute: %v", err)
	}
	second, err := distributor.Distribute(
		ctx,
		openDHTDurabilityGateState(),
		openDHTDurabilityGateConfig(),
	)
	if err != nil {
		t.Fatalf("second Distribute: %v", err)
	}
	if first.ConfirmedPostings != 0 || second.ConfirmedPostings != 1 || queue.PostingCount() != 0 {
		t.Fatalf("first/second/queue = %#v/%#v/%d", first, second, queue.PostingCount())
	}
	recovered, err := storage.outboundPostings.RecoverOutbound(ctx)
	if err != nil {
		t.Fatalf("RecoverOutbound: %v", err)
	}
	count, countErr := storage.postings.RWICount(ctx)
	if recovered != 0 || countErr != nil || count != 0 {
		t.Fatalf("recovered/count = %d/%d, %v", recovered, count, countErr)
	}
}

func TestDHTOutboundFeederFinalizesSelectedMetadataOrphan(t *testing.T) {
	ctx := t.Context()
	storage, err := openNodeStorage(openTestVault(t), "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	word := yagomodel.Hash("CCCCCCCCCCCC")
	url := yagomodel.Hash("DDDDDDDDDDDD")
	if _, err := storage.postingReceiver.Receive(ctx, []yagomodel.RWIPosting{
		dhtOutboundPosting(word, url),
	}); err != nil {
		t.Fatalf("store posting: %v", err)
	}
	source := dhtOutboundRWIWords{postings: storage.outboundPostings}
	queue := dhtexchange.NewOutboundQueue()
	peer := dhtOutboundPeer(t)
	receipt, err := dhtexchange.NewOutboundFeeder(
		queue,
		source,
		storage.urlDirectory,
		func(context.Context) []yagomodel.Seed { return []yagomodel.Seed{peer} },
		dhtexchange.OutboundFeederConfig{Redundancy: 1, MinimumPeerAgeDays: -1},
	).Feed(ctx)
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	recovered, recoverErr := storage.outboundPostings.RecoverOutbound(ctx)
	count, countErr := storage.postings.RWICount(ctx)
	if receipt.State != dhtexchange.OutboundFeedDropped || receipt.FinalizedPostings != 1 ||
		receipt.Enqueue.MissingURL != 1 || recovered != 0 || recoverErr != nil ||
		count != 0 || countErr != nil || queue.PostingCount() != 0 {
		t.Fatalf(
			"receipt/recovered/count/queue = %#v/%d,%v/%d,%v/%d",
			receipt,
			recovered,
			recoverErr,
			count,
			countErr,
			queue.PostingCount(),
		)
	}
}

func TestDHTOutboundFeederRestoresPostingWithoutEligibleTarget(t *testing.T) {
	ctx := t.Context()
	storage, err := openNodeStorage(openTestVault(t), "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	word := yagomodel.Hash("CCCCCCCCCCCC")
	url := yagomodel.Hash("DDDDDDDDDDDD")
	storeSenderDHTRows(t, ctx, storage, word, url)
	source := dhtOutboundRWIWords{postings: storage.outboundPostings}
	queue := dhtexchange.NewOutboundQueue()
	receipt, err := dhtexchange.NewOutboundFeeder(
		queue,
		source,
		storage.urlDirectory,
		func(context.Context) []yagomodel.Seed { return nil },
		dhtexchange.OutboundFeederConfig{Redundancy: 1},
	).Feed(ctx)
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	recovered, recoverErr := storage.outboundPostings.RecoverOutbound(ctx)
	count, countErr := storage.postings.RWICount(ctx)
	if receipt.State != dhtexchange.OutboundFeedRestored || receipt.RestoredPostings != 1 ||
		receipt.FinalizedPostings != 0 || recovered != 0 || recoverErr != nil ||
		count != 1 || countErr != nil || queue.PostingCount() != 0 {
		t.Fatalf(
			"receipt/recovered/count/queue = %#v/%d,%v/%d,%v/%d",
			receipt,
			recovered,
			recoverErr,
			count,
			countErr,
			queue.PostingCount(),
		)
	}
}

func preparedRedundantDHTSelection(
	t *testing.T,
	ctx context.Context,
) (nodeStorage, dhtOutboundRWIWords, *dhtexchange.OutboundQueue) {
	t.Helper()

	storage, err := openNodeStorage(openTestVault(t), "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	word := yagomodel.Hash("CCCCCCCCCCCC")
	url := yagomodel.Hash("DDDDDDDDDDDD")
	storeSenderDHTRows(t, ctx, storage, word, url)
	source := dhtOutboundRWIWords{postings: storage.outboundPostings}
	queue := dhtexchange.NewOutboundQueue()
	first := dhtOutboundPeer(t)
	second := first
	second.Hash = yagomodel.Hash("FFFFFFFFFFFF")
	receipt, err := dhtexchange.NewOutboundFeeder(
		queue,
		source,
		storage.urlDirectory,
		func(context.Context) []yagomodel.Seed { return []yagomodel.Seed{first, second} },
		dhtexchange.OutboundFeederConfig{
			Redundancy: 2, MinimumPeerAgeDays: -1,
		},
	).Feed(ctx)
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if receipt.State != dhtexchange.OutboundFeedEnqueued || queue.PostingCount() != 2 {
		t.Fatalf("receipt/queue = %#v/%d", receipt, queue.PostingCount())
	}

	return storage, source, queue
}

func openDHTDurabilityGateState() dhtexchange.GateState {
	return dhtexchange.GateState{
		LocalPeerKnown:   true,
		ConnectedPeers:   1,
		LocalRWIWords:    1,
		LocalRWIKnown:    true,
		CrawlQueueKnown:  true,
		IndexQueueKnown:  true,
		StorageAvailable: true,
		StorageKnown:     true,
	}
}

func openDHTDurabilityGateConfig() dhtexchange.GateConfig {
	config := dhtexchange.DefaultGateConfig()
	config.MinimumConnectedPeer = 1
	config.MinimumRWIWord = 1

	return config
}
