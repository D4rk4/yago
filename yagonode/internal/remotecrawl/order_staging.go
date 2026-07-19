package remotecrawl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const maximumRemoteDescriptionBytes = 1024

func (b *Broker) StageOrder(ctx context.Context, order yagocrawlcontract.CrawlOrder) error {
	staged := 0
	for _, request := range order.Requests {
		mode, valid := yagocrawlcontract.NormalizeCrawlRequestMode(request.Mode)
		if !valid || mode != yagocrawlcontract.CrawlRequestModeURL {
			continue
		}
		canonical, err := b.policy.Admit(ctx, request.URL)
		if err != nil {
			b.observe(ctx, Observation{Action: "stage", Outcome: "destination_rejected"}, true)
			continue
		}
		urlHash, _ := yagomodel.HashURL(canonical)
		record := queueRecord{
			URL: canonical, URLHash: urlHash.String(),
			Referrer: boundedString(request.ReferrerURL, MaximumReceiptURLBytes),
			Description: boundedString(
				request.AnchorName,
				maximumRemoteDescriptionBytes,
			),
			PublishedAt: publicationUnixNano(request.AppDate),
			State:       queueStatePending,
		}
		added, err := b.stageRecord(ctx, record)
		if err != nil {
			b.observe(ctx, Observation{
				Action: "stage", Outcome: "queue_failed", URLHash: urlHash.Hash(),
			}, true)

			return err
		}
		if added {
			staged++
		}
	}
	b.observe(ctx, Observation{Action: "stage", Outcome: "accepted", Count: staged}, false)

	return nil
}

func publicationUnixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}

	return value.UTC().UnixNano()
}

func (b *Broker) stageRecord(ctx context.Context, record queueRecord) (bool, error) {
	added := false
	err := b.storage.Update(ctx, func(tx *vault.Txn) error {
		if b.urlSequences.Contains(tx, vault.Key(record.URLHash)) {
			return nil
		}
		length, err := b.orders.Len(tx)
		if err != nil {
			return fmt.Errorf("read remote crawl queue depth: %w", err)
		}
		if length >= b.config.QueueCapacity {
			return ErrQueueFull
		}
		sequence, _, err := b.sequence.Get(tx, nextSequenceKey)
		if err != nil {
			return fmt.Errorf("read remote crawl sequence: %w", err)
		}
		record.Sequence = sequence
		if err := b.orders.Put(tx, sequenceKey(sequence), record); err != nil {
			return fmt.Errorf("store remote crawl order: %w", err)
		}
		if err := b.pending.Put(
			tx,
			sequenceKey(sequence),
			pendingRecord{Sequence: sequence},
		); err != nil {
			return fmt.Errorf("index pending remote crawl order: %w", err)
		}
		if err := b.urlSequences.Put(tx, vault.Key(record.URLHash), sequence); err != nil {
			return fmt.Errorf("store remote crawl URL sequence: %w", err)
		}
		if err := b.sequence.Put(tx, nextSequenceKey, sequence+1); err != nil {
			return fmt.Errorf("advance remote crawl sequence: %w", err)
		}
		added = true

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("stage remote crawl order: %w", err)
	}

	return added, nil
}

func boundedString(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return value[:maximum]
	}

	return value
}
