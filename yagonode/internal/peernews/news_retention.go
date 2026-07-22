package peernews

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	maximumNewsQueueRecords = 4096
	maximumNewsQueueBytes   = 4 << 20
	maximumKnownNewsRecords = 4096
	defaultNewsLifetime     = 24 * time.Hour
	extendedNewsLifetime    = 3 * 24 * time.Hour
	maximumNewsFutureSkew   = 5 * time.Minute
	newsRetentionTimeout    = 30 * time.Second
)

type newsRetention struct {
	queueRecords int
	queueBytes   int
	knownRecords int
}

type retainedNewsRecord struct {
	key     vault.Key
	tie     vault.Key
	created time.Time
	bytes   int
	index   int
}

func newsRecordAdmitted(record Record) bool {
	return len(record.WireForm()) <= maximumNewsRecordBytes
}

func newsPublicationAdmitted(record Record) bool {
	record.Distributed = distributionLimit

	return newsRecordAdmitted(record)
}

func newsLifetime(category string) time.Duration {
	if category == knownMarker || category == CategoryProfileUpdate ||
		category == CategoryCrawlStart {
		return extendedNewsLifetime
	}

	return defaultNewsLifetime
}

func newsExpired(created, now time.Time, category string) bool {
	return now.Sub(created) > newsLifetime(category)
}

func newsCreationAdmitted(created, now time.Time, category string) bool {
	return !created.After(now.Add(maximumNewsFutureSkew)) &&
		!newsExpired(created, now, category)
}

func (p *Pool) prune(ctx context.Context) error {
	if err := p.writePermit.Acquire(ctx); err != nil {
		return err
	}
	defer p.writePermit.Release()

	return p.pruneWhileHoldingWritePermit(ctx)
}

func (p *Pool) pruneWhileHoldingWritePermit(ctx context.Context) error {
	now := p.now().UTC()
	if err := p.pruneKnownNews(ctx, now); err != nil {
		return err
	}
	if err := p.pruneKnownNewsCategories(ctx); err != nil {
		return err
	}

	return p.pruneQueuedNews(ctx, now)
}

func newsIDCreation(id string) (time.Time, error) {
	if len(id) != len(newsTimestampLayout)+yagomodel.HashLength {
		return time.Time{}, fmt.Errorf("%w: news id %q", ErrBadNewsRecord, id)
	}
	if _, err := yagomodel.ParseHash(id[len(newsTimestampLayout):]); err != nil {
		return time.Time{}, fmt.Errorf("%w: news id %q: %w", ErrBadNewsRecord, id, err)
	}
	created, err := exactNewsTime(id[:len(newsTimestampLayout)])
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: news id %q: %w", ErrBadNewsRecord, id, err)
	}

	return created, nil
}
