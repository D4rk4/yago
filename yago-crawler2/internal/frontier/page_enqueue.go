package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	bolt "go.etcd.io/bbolt"
)

func (frontier *Frontier) Enqueue(
	ctx context.Context,
	runID, rawURL, referrerURL string,
	depth int,
	notBefore time.Time,
) (Page, bool, error) {
	return frontier.enqueue(ctx, runID, rawURL, referrerURL, depth, notBefore, 0, 0)
}

func (frontier *Frontier) EnqueueLimited(
	ctx context.Context,
	runID, rawURL, referrerURL string,
	depth int,
	notBefore time.Time,
	maximumHostPages int,
) (Page, bool, error) {
	return frontier.enqueue(ctx, runID, rawURL, referrerURL, depth, notBefore, maximumHostPages, 0)
}

func (frontier *Frontier) EnqueueRunLimited(
	ctx context.Context,
	runID, rawURL, referrerURL string,
	depth int,
	notBefore time.Time,
	maximumHostPages int,
	maximumRunPages uint64,
) (Page, bool, error) {
	return frontier.enqueue(
		ctx,
		runID,
		rawURL,
		referrerURL,
		depth,
		notBefore,
		maximumHostPages,
		maximumRunPages,
	)
}

func (frontier *Frontier) enqueue(
	ctx context.Context,
	runID, rawURL, referrerURL string,
	depth int,
	notBefore time.Time,
	maximumHostPages int,
	maximumRunPages uint64,
) (Page, bool, error) {
	if err := checkContext(ctx); err != nil {
		return Page{}, false, err
	}
	if runID == "" || depth < 0 || maximumHostPages < 0 {
		return Page{}, false, errors.New("invalid crawl page")
	}
	canonicalURL, ok := yagocrawlcontract.CanonicalURL(rawURL)
	if !ok {
		return Page{}, false, errors.New("invalid crawl page URL")
	}
	parsed, err := url.Parse(canonicalURL)
	if err != nil || parsed.Hostname() == "" {
		return Page{}, false, errors.New("invalid crawl page host")
	}
	if notBefore.IsZero() {
		notBefore = time.Now()
	}
	page := Page{
		RunID:       runID,
		URL:         canonicalURL,
		Host:        strings.ToLower(parsed.Hostname()),
		ReferrerURL: referrerURL,
		Depth:       depth,
		State:       StateQueued,
		NotBefore:   notBefore.UTC(),
	}
	identity := sha256.Sum256([]byte(runID + "\x00" + canonicalURL))
	page.ID = hex.EncodeToString(identity[:])
	added := false
	err = frontier.database.Update(func(transaction *bolt.Tx) error {
		pages := transaction.Bucket([]byte(pagesBucketName))
		if existing := pages.Get([]byte(page.ID)); existing != nil {
			return nil
		}
		metadata := transaction.Bucket([]byte(metadataBucketName))
		depth, err := readQueueDepth(metadata)
		if err != nil {
			return err
		}
		runs := transaction.Bucket([]byte(runsBucketName))
		runDepth, err := readRunDepth(runs, runID)
		if err != nil {
			return err
		}
		if maximumRunPages > 0 && runDepth.Total() >= maximumRunPages {
			return ErrRunPageLimit
		}
		runHosts, err := transaction.Bucket([]byte(runHostsBucketName)).CreateBucketIfNotExists([]byte(runID))
		if err != nil {
			return err
		}
		hostPages, err := readHostPageCount(runHosts, page.Host)
		if err != nil {
			return err
		}
		if maximumHostPages > 0 && hostPages >= uint64(maximumHostPages) {
			return ErrHostPageLimit
		}
		if frontier.config.MaximumPendingPages > 0 &&
			depth.Queued+depth.Fetching+depth.Deferred >= frontier.config.MaximumPendingPages {
			return ErrPendingLimit
		}
		ready, err := transaction.Bucket([]byte(readyBucketName)).CreateBucketIfNotExists([]byte(runID))
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
		if err := writeQueueState(metadata, StateQueued, depth.Queued+1); err != nil {
			return err
		}
		runDepth.Queued++
		if err := writeRunDepth(runs, runID, runDepth); err != nil {
			return err
		}
		if err := writeHostPageCount(runHosts, page.Host, hostPages+1); err != nil {
			return err
		}
		if frontier.config.MaximumStateBytes > 0 &&
			uint64(transaction.Size()) > frontier.config.MaximumStateBytes {
			return ErrStateLimit
		}
		added = true
		return nil
	})
	if err != nil {
		return Page{}, false, fmt.Errorf("enqueue crawl page: %w", err)
	}
	return page, added, nil
}

func readHostPageCount(hosts *bolt.Bucket, host string) (uint64, error) {
	raw := hosts.Get([]byte(host))
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, errors.New("invalid crawl run host page count")
	}
	var count uint64
	for _, value := range raw {
		count = count<<8 | uint64(value)
	}
	return count, nil
}

func writeHostPageCount(hosts *bolt.Bucket, host string, count uint64) error {
	encoded := make([]byte, 8)
	for index := 0; index < 8; index++ {
		encoded[index] = byte(count >> (56 - 8*index))
	}
	return hosts.Put([]byte(host), encoded)
}

func readyKey(due time.Time, sequence uint64) []byte {
	key := make([]byte, 16)
	encodedTime := uint64(due.UnixNano()) ^ (uint64(1) << 63)
	for index := 0; index < 8; index++ {
		key[index] = byte(encodedTime >> (56 - 8*index))
	}
	for index := 0; index < 8; index++ {
		key[8+index] = byte(sequence >> (56 - 8*index))
	}
	return key
}
