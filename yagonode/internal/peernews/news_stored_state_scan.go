package peernews

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (p *Pool) readStoredNewsState(
	ctx context.Context,
	tx *vault.Txn,
	state *newsStoredState,
	knownNews *boundedNewestNews,
	queuedNews *boundedNewestNews,
) error {
	var err error
	state.knownRecords, err = p.known.Len(tx)
	if err != nil {
		return fmt.Errorf("count known news: %w", err)
	}
	state.queueRecords, err = p.queue.Len(tx)
	if err != nil {
		return fmt.Errorf("count queued news: %w", err)
	}
	if err := p.indexKnownNewsRetention(ctx, tx, knownNews); err != nil {
		return err
	}
	if err := p.indexQueuedNewsRetention(ctx, tx, state, queuedNews); err != nil {
		return err
	}

	return nil
}

func (p *Pool) indexKnownNewsRetention(
	ctx context.Context,
	tx *vault.Txn,
	knownNews *boundedNewestNews,
) error {
	if err := p.known.Scan(tx, nil, func(key vault.Key, _ string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("index known news retention: %w", err)
		}
		created, err := newsIDCreation(string(key))
		if err != nil {
			return false, err
		}
		knownNews.Add(retainedNewsRecord{key: key, tie: key, created: created})

		return true, nil
	}); err != nil {
		return fmt.Errorf("index known news retention: %w", err)
	}

	return nil
}

func (p *Pool) indexQueuedNewsRetention(
	ctx context.Context,
	tx *vault.Txn,
	state *newsStoredState,
	queuedNews *boundedNewestNews,
) error {
	if err := p.queue.Scan(tx, nil, func(key vault.Key, wire string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("index queued news retention: %w", err)
		}
		queue, valid := parseQueueKey(key)
		record, err := parseRecord(wire, time.Time{})
		if err != nil || !valid {
			return false, fmt.Errorf("index queued news %q", key)
		}
		state.queueBytes += len(wire)
		queuedNews.Add(retainedNewsRecord{
			key: key, tie: vault.Key(record.ID() + "\x00" + string(queue)),
			created: record.Created, bytes: len(wire),
		})

		return true, nil
	}); err != nil {
		return fmt.Errorf("index queued news retention: %w", err)
	}

	return nil
}
