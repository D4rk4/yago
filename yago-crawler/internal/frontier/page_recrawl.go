package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	bbolt "go.etcd.io/bbolt"
)

func (frontier *Frontier) RequeueForRecrawl(
	ctx context.Context,
	runID string,
	rawURL string,
	now time.Time,
	minimumAge time.Duration,
) (bool, error) {
	if err := checkContext(ctx); err != nil {
		return false, err
	}
	if runID == "" || rawURL == "" || now.IsZero() || minimumAge < 0 {
		return false, errors.New("invalid recrawl request")
	}
	canonicalURL, ok := yagocrawlcontract.CanonicalURL(rawURL)
	if !ok {
		return false, errors.New("invalid recrawl URL")
	}
	identity := sha256.Sum256([]byte(runID + "\x00" + canonicalURL))
	pageID := hex.EncodeToString(identity[:])
	queued := false
	err := frontier.database.Update(func(transaction *bbolt.Tx) error {
		pages := transaction.Bucket([]byte(pagesBucketName))
		rawPage := pages.Get([]byte(pageID))
		if rawPage == nil {
			return nil
		}
		page, err := loadPage(rawPage)
		if err != nil {
			return err
		}
		if page.State != StateFetched && page.State != StateIndexed {
			return nil
		}
		if page.CompletedAt.IsZero() || now.Sub(page.CompletedAt) < minimumAge {
			return nil
		}
		if transaction.Bucket([]byte(ingestBucketName)).Get([]byte(page.ID)) != nil {
			return nil
		}
		metadata := transaction.Bucket([]byte(metadataBucketName))
		depth, err := readQueueDepth(metadata)
		if err != nil {
			return err
		}
		if frontier.config.MaximumPendingPages > 0 &&
			depth.Queued+depth.Fetching+depth.Deferred >= frontier.config.MaximumPendingPages {
			return ErrPendingLimit
		}
		runs := transaction.Bucket([]byte(runsBucketName))
		runDepth, err := readRunDepth(runs, page.RunID)
		if err != nil {
			return err
		}
		completedState := page.State
		if err := decrementState(&depth, page.State); err != nil {
			return err
		}
		if err := decrementState(&runDepth, page.State); err != nil {
			return err
		}
		depth.Queued++
		runDepth.Queued++
		page.State = StateQueued
		page.NotBefore = now.UTC()
		page.LeaseUntil = time.Time{}
		ready, err := transaction.Bucket([]byte(readyBucketName)).CreateBucketIfNotExists([]byte(page.RunID))
		if err != nil {
			return err
		}
		sequence, err := ready.NextSequence()
		if err != nil {
			return err
		}
		if err := ready.Put(readyKey(page.NotBefore, sequence), []byte(page.ID)); err != nil {
			return err
		}
		if err := savePage(pages, page); err != nil {
			return err
		}
		if err := writeQueueState(metadata, StateQueued, depth.Queued); err != nil {
			return err
		}
		if err := writeQueueState(metadata, completedState, depth.For(completedState)); err != nil {
			return err
		}
		if err := writeRunDepth(runs, page.RunID, runDepth); err != nil {
			return err
		}
		if frontier.config.MaximumStateBytes > 0 &&
			uint64(transaction.Size()) > frontier.config.MaximumStateBytes {
			return ErrStateLimit
		}
		queued = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("requeue page for recrawl: %w", err)
	}
	return queued, nil
}

func decrementState(depth *QueueDepth, state State) error {
	value := depth.For(state)
	if value == 0 {
		return fmt.Errorf("%s page count underflow", state)
	}
	switch state {
	case StateFetched:
		depth.Fetched--
	case StateIndexed:
		depth.Indexed--
	default:
		return fmt.Errorf("%s pages cannot be requeued for recrawl", state)
	}
	return nil
}
