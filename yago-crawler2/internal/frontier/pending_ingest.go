package frontier

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	bolt "go.etcd.io/bbolt"
)

type PendingIngest struct {
	PageID    string
	RunID     string
	LeaseID   string
	URL       string
	BatchJSON []byte
	Attempts  uint32
	NotBefore time.Time
}

type pendingIngestRecord struct {
	PageID    string          `json:"pageId"`
	RunID     string          `json:"runId"`
	LeaseID   string          `json:"leaseId"`
	BatchJSON json.RawMessage `json:"batchJson"`
	Attempts  uint32          `json:"attempts"`
	NotBefore time.Time       `json:"notBefore"`
}

func (frontier *Frontier) StorePendingIngest(
	ctx context.Context,
	pageID string,
	leaseID string,
	batchJSON []byte,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if pageID == "" || leaseID == "" || len(batchJSON) == 0 ||
		len(batchJSON) > yagocrawlcontract.MaximumIngestBatchBytes || !json.Valid(batchJSON) {
		return errors.New("invalid pending crawler ingest")
	}
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		pages := transaction.Bucket([]byte(pagesBucketName))
		page, err := loadPage(pages.Get([]byte(pageID)))
		if err != nil {
			return err
		}
		if page.State != StateFetching {
			return fmt.Errorf("page %s is not fetching", page.ID)
		}
		records := transaction.Bucket([]byte(ingestBucketName))
		previousJSON := records.Get([]byte(pageID))
		if previousJSON != nil {
			previous, err := decodePendingIngest(previousJSON)
			if err != nil {
				return err
			}
			if previous.RunID != page.RunID {
				return errors.New("pending crawler ingest run identity changed")
			}
			if err := transaction.Bucket([]byte(ingestReadyBucketName)).Delete(
				pendingIngestReadyKey(previous.NotBefore, previous.PageID),
			); err != nil {
				return err
			}
		}
		pending := pendingIngestRecord{
			PageID:    pageID,
			RunID:     page.RunID,
			LeaseID:   leaseID,
			BatchJSON: append(json.RawMessage(nil), batchJSON...),
			NotBefore: time.Now().UTC(),
		}
		encoded, err := json.Marshal(pending)
		if err != nil {
			return fmt.Errorf("encode pending crawler ingest: %w", err)
		}
		if err := records.Put([]byte(pageID), encoded); err != nil {
			return err
		}
		if previousJSON == nil {
			if err := addPendingIngestRunEntry(
				transaction.Bucket([]byte(ingestRunsBucketName)),
				pending.RunID,
				pending.PageID,
			); err != nil {
				return err
			}
		}
		if err := transaction.Bucket([]byte(ingestReadyBucketName)).Put(
			pendingIngestReadyKey(pending.NotBefore, pageID), []byte(pageID),
		); err != nil {
			return err
		}
		if frontier.config.MaximumStateBytes > 0 &&
			uint64(transaction.Size()) > frontier.config.MaximumStateBytes {
			return ErrStateLimit
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store pending crawler ingest: %w", err)
	}
	return nil
}

func (frontier *Frontier) NextPendingIngest(
	ctx context.Context,
	now time.Time,
) (PendingIngest, bool, error) {
	return frontier.NextPendingIngestSkipping(ctx, now, nil)
}

func (frontier *Frontier) NextPendingIngestSkipping(
	ctx context.Context,
	now time.Time,
	excluded map[string]struct{},
) (PendingIngest, bool, error) {
	if err := checkContext(ctx); err != nil {
		return PendingIngest{}, false, err
	}
	var pending PendingIngest
	found := false
	err := frontier.database.View(func(transaction *bolt.Tx) error {
		ready := transaction.Bucket([]byte(ingestReadyBucketName))
		records := transaction.Bucket([]byte(ingestBucketName))
		pages := transaction.Bucket([]byte(pagesBucketName))
		cursor := ready.Cursor()
		for key, pageID := cursor.First(); key != nil; key, pageID = cursor.Next() {
			if err := checkContext(ctx); err != nil {
				return err
			}
			notBefore, valid := pendingIngestReadyTime(key)
			if !valid {
				return errors.New("invalid pending crawler ingest schedule")
			}
			if notBefore.After(now) {
				break
			}
			if _, alreadySending := excluded[string(pageID)]; alreadySending {
				continue
			}
			raw := records.Get(pageID)
			if raw == nil {
				return errors.New("pending crawler ingest schedule has no record")
			}
			record, err := decodePendingIngest(raw)
			if err != nil {
				return err
			}
			page, err := loadPage(pages.Get(pageID))
			if err != nil {
				return err
			}
			if record.LeaseID == "" || page.State != StateFetched {
				continue
			}
			pending = PendingIngest{
				PageID:    record.PageID,
				RunID:     record.RunID,
				LeaseID:   record.LeaseID,
				URL:       page.URL,
				BatchJSON: append([]byte(nil), record.BatchJSON...),
				Attempts:  record.Attempts,
				NotBefore: record.NotBefore,
			}
			found = true
			break
		}
		return nil
	})
	if err != nil {
		return PendingIngest{}, false, fmt.Errorf("read pending crawler ingest: %w", err)
	}
	return pending, found, nil
}

func (frontier *Frontier) RetryPendingIngest(
	ctx context.Context,
	pageID string,
	notBefore time.Time,
	clearLease bool,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		records := transaction.Bucket([]byte(ingestBucketName))
		raw := records.Get([]byte(pageID))
		if raw == nil {
			return nil
		}
		record, err := decodePendingIngest(raw)
		if err != nil {
			return err
		}
		ready := transaction.Bucket([]byte(ingestReadyBucketName))
		if err := ready.Delete(pendingIngestReadyKey(record.NotBefore, record.PageID)); err != nil {
			return err
		}
		record.Attempts++
		record.NotBefore = notBefore.UTC()
		if clearLease {
			record.LeaseID = ""
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("encode pending crawler ingest retry: %w", err)
		}
		if err := records.Put([]byte(pageID), encoded); err != nil {
			return err
		}
		return ready.Put(pendingIngestReadyKey(record.NotBefore, record.PageID), []byte(pageID))
	})
	if err != nil {
		return fmt.Errorf("schedule pending crawler ingest retry: %w", err)
	}
	return nil
}

func (frontier *Frontier) RebindPendingIngestLease(
	ctx context.Context,
	runID string,
	leaseID string,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if runID == "" || leaseID == "" {
		return errors.New("pending crawler ingest lease identity is required")
	}
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		records := transaction.Bucket([]byte(ingestBucketName))
		runs := transaction.Bucket([]byte(ingestRunsBucketName))
		run := runs.Bucket([]byte(runID))
		if run == nil {
			return nil
		}
		ready := transaction.Bucket([]byte(ingestReadyBucketName))
		cursor := run.Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			if err := checkContext(ctx); err != nil {
				return err
			}
			if string(key) == pendingIngestRunCountKey {
				continue
			}
			raw := records.Get(key)
			if raw == nil {
				return errors.New("pending crawler ingest run index has no record")
			}
			record, err := decodePendingIngest(raw)
			if err != nil {
				return err
			}
			if err := ready.Delete(pendingIngestReadyKey(record.NotBefore, record.PageID)); err != nil {
				return err
			}
			record.LeaseID = leaseID
			record.NotBefore = time.Now().UTC()
			encoded, err := json.Marshal(record)
			if err != nil {
				return fmt.Errorf("encode rebound crawler ingest: %w", err)
			}
			if err := records.Put(key, encoded); err != nil {
				return err
			}
			if err := ready.Put(pendingIngestReadyKey(record.NotBefore, record.PageID), []byte(record.PageID)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("rebind pending crawler ingest lease: %w", err)
	}
	return nil
}

func (frontier *Frontier) CompletePendingIngest(
	ctx context.Context,
	pageID string,
	indexed bool,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		records := transaction.Bucket([]byte(ingestBucketName))
		raw := records.Get([]byte(pageID))
		if raw == nil {
			return nil
		}
		record, err := decodePendingIngest(raw)
		if err != nil {
			return err
		}
		if indexed {
			pages := transaction.Bucket([]byte(pagesBucketName))
			page, err := loadPage(pages.Get([]byte(pageID)))
			if err != nil {
				return err
			}
			if page.State != StateFetched {
				return fmt.Errorf("page %s is not awaiting ingest", pageID)
			}
			metadata := transaction.Bucket([]byte(metadataBucketName))
			depth, err := readQueueDepth(metadata)
			if err != nil {
				return err
			}
			runs := transaction.Bucket([]byte(runsBucketName))
			runDepth, err := readRunDepth(runs, record.RunID)
			if err != nil {
				return err
			}
			if depth.Fetched == 0 || runDepth.Fetched == 0 {
				return errors.New("fetched page count underflow")
			}
			depth.Fetched--
			depth.Indexed++
			runDepth.Fetched--
			runDepth.Indexed++
			page.State = StateIndexed
			if err := savePage(pages, page); err != nil {
				return err
			}
			if err := writeQueueState(metadata, StateFetched, depth.Fetched); err != nil {
				return err
			}
			if err := writeQueueState(metadata, StateIndexed, depth.Indexed); err != nil {
				return err
			}
			if err := writeRunDepth(runs, record.RunID, runDepth); err != nil {
				return err
			}
		}
		if err := transaction.Bucket([]byte(ingestReadyBucketName)).Delete(
			pendingIngestReadyKey(record.NotBefore, record.PageID),
		); err != nil {
			return err
		}
		if err := removePendingIngestRunEntry(
			transaction.Bucket([]byte(ingestRunsBucketName)),
			record.RunID,
			record.PageID,
		); err != nil {
			return err
		}
		return records.Delete([]byte(pageID))
	})
	if err != nil {
		return fmt.Errorf("complete pending crawler ingest: %w", err)
	}
	return nil
}

func (frontier *Frontier) PendingIngestForRun(
	ctx context.Context,
	runID string,
) (uint64, error) {
	if err := checkContext(ctx); err != nil {
		return 0, err
	}
	var count uint64
	err := frontier.database.View(func(transaction *bolt.Tx) error {
		run := transaction.Bucket([]byte(ingestRunsBucketName)).Bucket([]byte(runID))
		if run == nil {
			return nil
		}
		var err error
		count, err = pendingIngestRunCount(run)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("count pending crawler ingests: %w", err)
	}
	return count, nil
}

func decodePendingIngest(raw []byte) (pendingIngestRecord, error) {
	var record pendingIngestRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return pendingIngestRecord{}, fmt.Errorf("decode pending crawler ingest: %w", err)
	}
	if record.PageID == "" || record.RunID == "" || len(record.BatchJSON) == 0 ||
		!json.Valid(record.BatchJSON) || record.NotBefore.IsZero() {
		return pendingIngestRecord{}, errors.New("invalid pending crawler ingest")
	}
	return record, nil
}

func pendingIngestReadyKey(notBefore time.Time, pageID string) []byte {
	key := make([]byte, 8+len(pageID))
	binary.BigEndian.PutUint64(key[:8], uint64(notBefore.UnixNano()))
	copy(key[8:], pageID)
	return key
}

func pendingIngestReadyTime(key []byte) (time.Time, bool) {
	if len(key) < 9 {
		return time.Time{}, false
	}
	return time.Unix(0, int64(binary.BigEndian.Uint64(key[:8]))).UTC(), true
}
