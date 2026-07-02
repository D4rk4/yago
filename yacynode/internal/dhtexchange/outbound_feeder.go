package dhtexchange

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

type OutboundFeedState string

const (
	OutboundFeedSkipped  OutboundFeedState = "skipped"
	OutboundFeedEmpty    OutboundFeedState = "empty"
	OutboundFeedEnqueued OutboundFeedState = "enqueued"
	OutboundFeedDropped  OutboundFeedState = "dropped"
	OutboundFeedRestored OutboundFeedState = "restored"
)

type OutboundWordSource interface {
	SelectOutboundWords(
		ctx context.Context,
		maxWords int,
		maxPostings int,
	) ([]yacymodel.WordPostings, error)
	RestoreOutboundWords(ctx context.Context, words []yacymodel.WordPostings) (int, error)
}

type PeerSnapshot func(context.Context) []yacymodel.Seed

type OutboundQueueFeeder interface {
	Feed(context.Context) (OutboundFeedReceipt, error)
}

type OutboundFeederConfig struct {
	MaxWords           int
	MaxPostings        int
	Redundancy         int
	PartitionExponent  int
	MinimumPeerAgeDays int
	Now                func() time.Time
}

type OutboundFeedReceipt struct {
	State            OutboundFeedState
	SelectedPostings int
	RestoredPostings int
	Enqueue          EnqueueReceipt
}

type OutboundFeeder struct {
	queue  *OutboundQueue
	source OutboundWordSource
	urls   URLDirectory
	peers  PeerSnapshot
	config OutboundFeederConfig
}

func NewOutboundFeeder(
	queue *OutboundQueue,
	source OutboundWordSource,
	urls URLDirectory,
	peers PeerSnapshot,
	config OutboundFeederConfig,
) OutboundFeeder {
	if config.MaxWords <= 0 {
		config.MaxWords = 1
	}
	if config.MaxPostings <= 0 || config.MaxPostings > MaxChunkPostings {
		config.MaxPostings = MaxChunkPostings
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	return OutboundFeeder{
		queue:  queue,
		source: source,
		urls:   urls,
		peers:  peers,
		config: config,
	}
}

func (f OutboundFeeder) Feed(ctx context.Context) (OutboundFeedReceipt, error) {
	if f.queue.Len() > 0 {
		return OutboundFeedReceipt{State: OutboundFeedSkipped}, nil
	}

	words, err := f.source.SelectOutboundWords(
		ctx,
		f.config.MaxWords,
		f.config.MaxPostings,
	)
	receipt := OutboundFeedReceipt{
		State:            OutboundFeedEmpty,
		SelectedPostings: wordPostingCount(words),
	}
	if err != nil {
		return receipt, fmt.Errorf("select outbound rwi: %w", err)
	}
	if len(words) == 0 {
		return receipt, nil
	}

	peers := f.peers(ctx)
	restorable := make([]yacymodel.WordPostings, 0, len(words))
	for _, word := range words {
		enqueued, err := f.queue.EnqueueWord(
			ctx,
			f.urls,
			peers,
			word,
			EnqueueConfig{
				Redundancy:         f.config.Redundancy,
				PartitionExponent:  f.config.PartitionExponent,
				MinimumPeerAgeDays: f.config.MinimumPeerAgeDays,
				Now:                f.config.Now(),
			},
		)
		receipt.Enqueue = addEnqueueReceipts(receipt.Enqueue, enqueued)
		if err != nil {
			return f.restore(ctx, words, receipt, fmt.Errorf("enqueue outbound rwi: %w", err))
		}
		restorable = appendOutboundRestorable(restorable, word.WordHash, enqueued.acceptedRows)
	}

	if receipt.Enqueue.TargetCopies > 0 && receipt.Enqueue.OverflowCopies == 0 {
		receipt.State = OutboundFeedEnqueued

		return receipt, nil
	}
	if receipt.Enqueue.AcceptedPostings == 0 {
		receipt.State = OutboundFeedDropped

		return receipt, nil
	}

	return f.restore(ctx, restorable, receipt, nil)
}

func (f OutboundFeeder) restore(
	ctx context.Context,
	words []yacymodel.WordPostings,
	receipt OutboundFeedReceipt,
	cause error,
) (OutboundFeedReceipt, error) {
	f.queue.Clear()
	restored, err := f.source.RestoreOutboundWords(ctx, words)
	receipt.State = OutboundFeedRestored
	receipt.RestoredPostings = restored
	if err != nil {
		err = fmt.Errorf("restore outbound rwi: %w", err)
	}

	return receipt, errors.Join(cause, err)
}

func addEnqueueReceipts(a, b EnqueueReceipt) EnqueueReceipt {
	return EnqueueReceipt{
		AcceptedPostings: a.AcceptedPostings + b.AcceptedPostings,
		MissingURL:       a.MissingURL + b.MissingURL,
		BadPostings:      a.BadPostings + b.BadPostings,
		TargetCopies:     a.TargetCopies + b.TargetCopies,
		OverflowCopies:   a.OverflowCopies + b.OverflowCopies,
		TouchedChunks:    a.TouchedChunks + b.TouchedChunks,
		acceptedRows:     append(a.acceptedRows, b.acceptedRows...),
	}
}

func appendOutboundRestorable(
	words []yacymodel.WordPostings,
	word yacymodel.Hash,
	rows []yacymodel.RWIPosting,
) []yacymodel.WordPostings {
	if len(rows) == 0 {
		return words
	}

	return append(words, yacymodel.WordPostings{WordHash: word, Postings: rows})
}

func wordPostingCount(words []yacymodel.WordPostings) int {
	count := 0
	for _, word := range words {
		count += len(word.Postings)
	}

	return count
}
