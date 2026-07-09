// Package recrawlfrontier keeps a durable, node-side recrawl schedule: for each
// crawled URL it records when the page was last fetched and when it is next due
// for a recrawl, so pages are refetched on their profile's cadence and the
// schedule survives node and crawler restarts. It is fed by ingest completions
// (Observe) and drained by a sweeper that re-dispatches due URLs (ClaimDue).
package recrawlfrontier

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	recordBucket  vault.Name = "recrawl_records"
	dueBucket     vault.Name = "recrawl_due"
	profileBucket vault.Name = "recrawl_profiles"
)

// scheduleRecord is one URL's recrawl state, keyed by its URL hash.
type scheduleRecord struct {
	URL           string
	ProfileHandle string
	Interval      time.Duration
	NextDueAt     time.Time
}

type recordCodec struct{}

func (recordCodec) Encode(record scheduleRecord) ([]byte, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode recrawl record: %w", err)
	}

	return raw, nil
}

func (recordCodec) Decode(raw []byte) (scheduleRecord, error) {
	var record scheduleRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return scheduleRecord{}, fmt.Errorf("decode recrawl record: %w", err)
	}

	return record, nil
}

type profileCodec struct{}

func (profileCodec) Encode(profile yagocrawlcontract.CrawlProfile) ([]byte, error) {
	// CrawlProfile holds only string, int, bool and time.Duration fields, so
	// json.Marshal cannot fail; the error result satisfies the codec interface.
	raw, _ := json.Marshal(profile)

	return raw, nil
}

func (profileCodec) Decode(raw []byte) (yagocrawlcontract.CrawlProfile, error) {
	var profile yagocrawlcontract.CrawlProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return yagocrawlcontract.CrawlProfile{}, fmt.Errorf("decode recrawl profile: %w", err)
	}

	return profile, nil
}

// DueURL names a URL the sweeper should re-dispatch, with the profile that owns it.
type DueURL struct {
	URL           string
	ProfileHandle string
}

// Frontier is the durable recrawl schedule. It holds the per-URL records, an
// ordered due index so due URLs are found with a bounded forward scan, and the
// crawl profiles (keyed by handle) needed to interpret and re-dispatch them.
type Frontier struct {
	vault    *vault.Vault
	records  *vault.Collection[scheduleRecord]
	due      *vault.Collection[struct{}]
	profiles *vault.Collection[yagocrawlcontract.CrawlProfile]
}

func Open(v *vault.Vault) (*Frontier, error) {
	records, err := vault.Register(v, recordBucket, recordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register recrawl records: %w", err)
	}
	due, err := vault.Register(v, dueBucket, presenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register recrawl due index: %w", err)
	}
	profiles, err := vault.Register(v, profileBucket, profileCodec{})
	if err != nil {
		return nil, fmt.Errorf("register recrawl profiles: %w", err)
	}

	return &Frontier{vault: v, records: records, due: due, profiles: profiles}, nil
}

// Observe records that url was fetched at fetchedAt under a profile whose recrawl
// interval is interval. A non-positive interval means the profile never recrawls,
// so any existing schedule for the url is dropped. Otherwise the url is scheduled
// to recrawl at fetchedAt+interval, replacing any earlier schedule for it.
func (f *Frontier) Observe(
	ctx context.Context,
	url, profileHandle string,
	interval time.Duration,
	fetchedAt time.Time,
) error {
	if err := f.vault.Update(ctx, func(tx *vault.Txn) error {
		return f.observeInTx(tx, fetchObservation{
			url: url, profileHandle: profileHandle,
			interval: interval, fetchedAt: fetchedAt,
		})
	}); err != nil {
		return fmt.Errorf("observe recrawl url: %w", err)
	}

	return nil
}

// fetchObservation is one fetch to schedule, carried into a shared transaction so a
// whole ingest micro-batch commits its recrawl schedule once (IO-AGG-01).
type fetchObservation struct {
	url           string
	profileHandle string
	interval      time.Duration
	fetchedAt     time.Time
}

// observeInTx applies one fetch observation inside the caller's transaction.
func (f *Frontier) observeInTx(tx *vault.Txn, obs fetchObservation) error {
	// HashURL derives a fixed 12-character hash from any input, so it never
	// errors here; the error result exists only for the general URL API.
	hash, _ := yagomodel.HashURL(obs.url)
	key := vault.Key(string(hash))

	if err := f.clearDue(tx, key); err != nil {
		return err
	}
	if obs.interval <= 0 {
		if _, err := f.records.Delete(tx, key); err != nil {
			return fmt.Errorf("drop recrawl record: %w", err)
		}

		return nil
	}

	return f.schedule(tx, key, scheduleRecord{
		URL:           obs.url,
		ProfileHandle: obs.profileHandle,
		Interval:      obs.interval,
		NextDueAt:     obs.fetchedAt.Add(obs.interval).UTC(),
	})
}

// ClaimDue returns up to limit URLs whose next-due time is at or before now,
// soonest first, and atomically pushes each one's next-due forward by its
// interval so the same URL is not re-dispatched on every sweep. A URL that is
// actually recrawled is re-scheduled precisely by the next Observe.
func (f *Frontier) ClaimDue(ctx context.Context, now time.Time, limit int) ([]DueURL, error) {
	if limit <= 0 {
		return nil, nil
	}
	claimed := make([]DueURL, 0, limit)
	err := f.vault.Update(ctx, func(tx *vault.Txn) error {
		due, err := f.collectDue(tx, now, limit)
		if err != nil {
			return err
		}
		for _, entry := range due {
			if _, err := f.due.Delete(tx, entry.dueKey); err != nil {
				return fmt.Errorf("clear claimed recrawl due entry: %w", err)
			}
			entry.record.NextDueAt = now.Add(entry.record.Interval).UTC()
			if err := f.schedule(tx, entry.hashKey, entry.record); err != nil {
				return err
			}
			claimed = append(claimed, DueURL{
				URL:           entry.record.URL,
				ProfileHandle: entry.record.ProfileHandle,
			})
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claim due recrawls: %w", err)
	}

	return claimed, nil
}

type dueEntry struct {
	dueKey  vault.Key
	hashKey vault.Key
	record  scheduleRecord
}

// collectDue scans the due index in chronological order, stopping at the first
// entry due after now, and returns the due records without mutating the index so
// the caller can advance them after the scan completes.
func (f *Frontier) collectDue(tx *vault.Txn, now time.Time, limit int) ([]dueEntry, error) {
	var due []dueEntry
	err := f.due.Scan(tx, nil, func(key vault.Key, _ struct{}) (bool, error) {
		at, err := nextDueFromKey(key)
		if err != nil {
			return false, err
		}
		if at.After(now) {
			return false, nil
		}
		// nextDueFromKey above already validated the key's separator, so
		// hashFromDueKey performs the same split and cannot fail here.
		hash, _ := hashFromDueKey(key)
		record, found, err := f.records.Get(tx, vault.Key(hash))
		if err != nil {
			return false, fmt.Errorf("read due recrawl record: %w", err)
		}
		if found {
			due = append(due, dueEntry{
				dueKey:  append(vault.Key(nil), key...),
				hashKey: vault.Key(hash),
				record:  record,
			})
		}

		return len(due) < limit, nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan recrawl due index: %w", err)
	}

	return due, nil
}

// clearDue removes the url's current due-index entry (if any), using its stored
// next-due time so a stale index key is not orphaned when the schedule changes.
func (f *Frontier) clearDue(tx *vault.Txn, key vault.Key) error {
	existing, found, err := f.records.Get(tx, key)
	if err != nil {
		return fmt.Errorf("read recrawl record: %w", err)
	}
	if !found {
		return nil
	}
	if _, err := f.due.Delete(tx, dueKey(existing.NextDueAt, string(key))); err != nil {
		return fmt.Errorf("drop recrawl due entry: %w", err)
	}

	return nil
}

func (f *Frontier) schedule(tx *vault.Txn, key vault.Key, record scheduleRecord) error {
	if err := f.records.Put(tx, key, record); err != nil {
		return fmt.Errorf("write recrawl record: %w", err)
	}
	if err := f.due.Put(tx, dueKey(record.NextDueAt, string(key)), struct{}{}); err != nil {
		return fmt.Errorf("write recrawl due entry: %w", err)
	}

	return nil
}
