package searchdemand

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	collectionName  vault.Name = "search_demand_records"
	DefaultPageSize            = 128
	MaximumPageSize            = 512
)

var ErrInvalidDemand = errors.New("invalid search demand")

type Record struct {
	Topic          string    `json:"topic"`
	URL            string    `json:"url"`
	Shown          uint64    `json:"shown"`
	Clicked        uint64    `json:"clicked"`
	LastSeen       time.Time `json:"lastSeen"`
	LastClicked    time.Time `json:"lastClicked"`
	FocusedAt      time.Time `json:"focusedAt"`
	UpdateSequence uint64    `json:"updateSequence"`
}

type Page struct {
	Records    []Record
	NextCursor string
}

type Store struct {
	vault    *vault.Vault
	records  *vault.Collection[Record]
	updates  *vault.Keyspace[string]
	sequence *vault.Keyspace[uint64]
}

type recordCodec struct{ vault.JSONCodec[Record] }

func (recordCodec) Decode(raw []byte) (Record, error) {
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return Record{}, fmt.Errorf("decode search demand: %w", err)
	}
	return record, nil
}

func Open(storage *vault.Vault) (*Store, error) {
	if storage == nil {
		return nil, errors.New("open search demand: missing vault")
	}
	records, err := vault.Register(storage, collectionName, recordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register search demand: %w", err)
	}
	updates, err := vault.RegisterKeyspace(storage, updateIndexName, stringCodec{})
	if err != nil {
		return nil, fmt.Errorf("register search demand updates: %w", err)
	}
	sequence, err := vault.RegisterKeyspace(storage, updateSequenceName, sequenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register search demand sequence: %w", err)
	}
	store := &Store{vault: storage, records: records, updates: updates, sequence: sequence}
	if err := store.initializeUpdateIndex(context.Background()); err != nil {
		return nil, fmt.Errorf("initialize search demand updates: %w", err)
	}
	return store, nil
}

func (store *Store) RecordShown(ctx context.Context, topic, rawURL string, at time.Time) error {
	if _, ok := yagocrawlcontract.CanonicalURL(strings.TrimSpace(rawURL)); !ok {
		return ErrInvalidDemand
	}
	return store.RecordShownBatch(ctx, topic, []string{rawURL}, at)
}

func (store *Store) RecordShownBatch(ctx context.Context, topic string, rawURLs []string, at time.Time) error {
	topic = strings.Join(strings.Fields(strings.TrimSpace(topic)), " ")
	if topic == "" || at.IsZero() {
		return ErrInvalidDemand
	}
	type shownDemand struct {
		key  vault.Key
		url  string
		seen uint64
	}
	shown := make(map[string]shownDemand, len(rawURLs))
	for _, rawURL := range rawURLs {
		canonicalURL, ok := yagocrawlcontract.CanonicalURL(strings.TrimSpace(rawURL))
		if !ok {
			continue
		}
		key := demandKey(topic, canonicalURL)
		id := string(key)
		item := shown[id]
		item.key = key
		item.url = canonicalURL
		item.seen = increment(item.seen)
		shown[id] = item
	}
	if len(shown) == 0 {
		return nil
	}
	if err := store.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, item := range shown {
			record, found, err := store.records.Get(tx, item.key)
			if err != nil {
				return err
			}
			if !found {
				record.Topic = topic
				record.URL = item.url
			}
			record.Shown = incrementBy(record.Shown, item.seen)
			record.LastSeen = later(record.LastSeen, at)
			if err := store.recordUpdate(tx, item.key, record); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("record search demand: %w", err)
	}
	return nil
}

func (store *Store) RecordClicked(ctx context.Context, topic, rawURL string, at time.Time) error {
	return store.record(ctx, topic, rawURL, at, true, false)
}

func (store *Store) RecordFocused(ctx context.Context, topic, rawURL string, at time.Time) error {
	return store.RecordFocusedBatch(ctx, topic, []string{rawURL}, at)
}

func (store *Store) RecordFocusedBatch(
	ctx context.Context,
	topic string,
	rawURLs []string,
	at time.Time,
) error {
	topic = strings.Join(strings.Fields(strings.TrimSpace(topic)), " ")
	if topic == "" || at.IsZero() {
		return ErrInvalidDemand
	}
	urls := make(map[string]vault.Key, len(rawURLs))
	for _, rawURL := range rawURLs {
		canonicalURL, ok := yagocrawlcontract.CanonicalURL(strings.TrimSpace(rawURL))
		if !ok {
			return ErrInvalidDemand
		}
		urls[canonicalURL] = demandKey(topic, canonicalURL)
	}
	if len(urls) == 0 {
		return ErrInvalidDemand
	}
	canonicalURLs := make([]string, 0, len(urls))
	for canonicalURL := range urls {
		canonicalURLs = append(canonicalURLs, canonicalURL)
	}
	sort.Strings(canonicalURLs)
	if err := store.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, canonicalURL := range canonicalURLs {
			key := urls[canonicalURL]
			record, found, err := store.records.Get(tx, key)
			if err != nil {
				return err
			}
			if !found {
				record.Topic = topic
				record.URL = canonicalURL
			}
			record.FocusedAt = later(record.FocusedAt, at)
			if err := store.recordUpdate(tx, key, record); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("record focused search demand: %w", err)
	}
	return nil
}

func (store *Store) record(
	ctx context.Context,
	topic, rawURL string,
	at time.Time,
	clicked, focused bool,
) error {
	topic = strings.Join(strings.Fields(strings.TrimSpace(topic)), " ")
	canonicalURL, ok := yagocrawlcontract.CanonicalURL(strings.TrimSpace(rawURL))
	if topic == "" || !ok || at.IsZero() {
		return ErrInvalidDemand
	}
	key := demandKey(topic, canonicalURL)
	if err := store.vault.Update(ctx, func(tx *vault.Txn) error {
		record, found, err := store.records.Get(tx, key)
		if err != nil {
			return err
		}
		if !found {
			record.Topic = topic
			record.URL = canonicalURL
		}
		if clicked {
			record.Clicked = increment(record.Clicked)
			record.LastClicked = later(record.LastClicked, at)
		} else if !focused {
			record.Shown = increment(record.Shown)
			record.LastSeen = later(record.LastSeen, at)
		}
		if focused {
			record.FocusedAt = later(record.FocusedAt, at)
		}
		return store.recordUpdate(tx, key, record)
	}); err != nil {
		return fmt.Errorf("record search demand: %w", err)
	}
	return nil
}

func (store *Store) ReadPage(ctx context.Context, cursor string, pageSize int) (Page, error) {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaximumPageSize {
		return Page{}, fmt.Errorf("search demand page size outside range: %d", pageSize)
	}
	var after vault.Key
	if cursor != "" {
		decoded, err := hex.DecodeString(cursor)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != cursor {
			return Page{}, errors.New("invalid search demand cursor")
		}
		after = vault.Key(decoded)
	}
	page := Page{Records: make([]Record, 0, pageSize)}
	if err := store.vault.View(ctx, func(tx *vault.Txn) error {
		keys, err := tx.ReadBucketKeyPage(collectionName, after, pageSize+1)
		if err != nil {
			return err
		}
		more := keys.More || len(keys.Keys) > pageSize
		if more {
			keys.Keys = keys.Keys[:pageSize]
		}
		for _, key := range keys.Keys {
			record, found, err := store.records.Get(tx, key)
			if err != nil {
				return err
			}
			if !found {
				return errors.New("search demand key page contains a missing record")
			}
			page.Records = append(page.Records, record)
		}
		if more && len(keys.Keys) > 0 {
			page.NextCursor = hex.EncodeToString(keys.Keys[len(keys.Keys)-1])
		}
		return nil
	}); err != nil {
		return Page{}, fmt.Errorf("read search demand: %w", err)
	}
	return page, nil
}

func demandKey(topic, canonicalURL string) vault.Key {
	identity := sha256.Sum256([]byte(topic + "\x00" + canonicalURL))
	return vault.Key(identity[:])
}

func increment(value uint64) uint64 {
	if value < ^uint64(0) {
		return value + 1
	}
	return value
}

func incrementBy(value, amount uint64) uint64 {
	if ^uint64(0)-value < amount {
		return ^uint64(0)
	}
	return value + amount
}

func later(current, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate.UTC()
	}
	return current
}
