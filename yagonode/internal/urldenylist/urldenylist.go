// Package urldenylist keeps a durable, operator-managed denylist of URLs and
// whole domains. Denylisted entries are filtered out of search results so blocked
// content is never served, and the list survives restarts as a small vault-backed
// collection keyed by kind and value.
package urldenylist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const denyBucket vault.Name = "urldenylist"

// keySeparator joins the entry kind and value into a single collection key. A
// NUL byte cannot appear in a hostname or a URL, so the split is unambiguous.
const keySeparator = "\x00"

// Kind distinguishes an exact-URL entry from a whole-domain entry.
type Kind string

const (
	// KindURL blocks one exact URL.
	KindURL Kind = "url"
	// KindDomain blocks a host and every subdomain of it.
	KindDomain Kind = "domain"
)

type record struct {
	AddedAt time.Time `json:"addedAt"`
}

type recordCodec struct{}

func (recordCodec) Encode(rec record) ([]byte, error) {
	data, _ := json.Marshal(rec)

	return data, nil
}

func (recordCodec) Decode(raw []byte) (record, error) {
	var rec record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return record{}, fmt.Errorf("decode denylist record: %w", err)
	}

	return rec, nil
}

// Entry is one denylist entry with the time it was added.
type Entry struct {
	Kind    Kind
	Value   string
	AddedAt time.Time
}

// Store persists the operator's URL/domain denylist.
type Store struct {
	vault     *vault.Vault
	records   *vault.Collection[record]
	now       func() time.Time
	snapshots snapshotCache
}

// Open registers the denylist collection on the shared vault.
func Open(v *vault.Vault, now func() time.Time) (*Store, error) {
	records, err := vault.Register(v, denyBucket, recordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register url denylist: %w", err)
	}

	store := &Store{vault: v, records: records, now: now}
	snapshot, err := store.loadSnapshot(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load url denylist: %w", err)
	}
	store.snapshots.current.Store(&snapshot)

	return store, nil
}

// Add puts a URL or domain on the denylist. Adding an existing entry refreshes
// its recorded time and is not an error.
func (s *Store) Add(ctx context.Context, kind Kind, value string) error {
	value = normalize(kind, value)
	if value == "" {
		return fmt.Errorf("denylist %s value is empty", kind)
	}
	s.snapshots.mutations.Lock()
	defer s.snapshots.mutations.Unlock()

	rec := record{AddedAt: s.now().UTC()}
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := s.records.Put(tx, s.key(kind, value), rec); err != nil {
			return fmt.Errorf("store denylist entry: %w", err)
		}

		return nil
	}); err != nil {
		return s.reconcileFailedMutation(
			ctx,
			fmt.Errorf("update denylist: %w", err),
			kind,
			value,
			true,
		)
	}
	s.snapshots.storeAdded(kind, value)

	return nil
}

// Remove drops an entry, reporting whether it was present.
func (s *Store) Remove(ctx context.Context, kind Kind, value string) (bool, error) {
	value = normalize(kind, value)
	s.snapshots.mutations.Lock()
	defer s.snapshots.mutations.Unlock()

	var removed bool
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		deleted, err := s.records.Delete(tx, s.key(kind, value))
		if err != nil {
			return fmt.Errorf("delete denylist entry: %w", err)
		}
		removed = deleted

		return nil
	}); err != nil {
		return false, s.reconcileFailedMutation(
			ctx,
			fmt.Errorf("update denylist: %w", err),
			kind,
			value,
			false,
		)
	}
	s.snapshots.storeRemoved(kind, value)

	return removed, nil
}

// Entries returns every denylist entry sorted by kind then value.
func (s *Store) Entries(ctx context.Context) ([]Entry, error) {
	var entries []Entry
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.records.Scan(tx, nil, func(key vault.Key, rec record) (bool, error) {
			kind, value := splitKey(key)
			entries = append(entries, Entry{Kind: kind, Value: value, AddedAt: rec.AddedAt})

			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("view denylist: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}

		return entries[i].Value < entries[j].Value
	})

	return entries, nil
}

// Snapshot is an in-memory copy of the denylist for fast per-result matching
// within a single search, avoiding a store read per candidate result.
type Snapshot struct {
	urls    map[string]struct{}
	domains map[string]struct{}
}

func (s *Store) Snapshot() Snapshot {
	return *s.snapshots.current.Load()
}

func (s *Store) loadSnapshot(ctx context.Context) (Snapshot, error) {
	snap := Snapshot{urls: map[string]struct{}{}, domains: map[string]struct{}{}}
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.records.Scan(tx, nil, func(key vault.Key, _ record) (bool, error) {
			switch kind, value := splitKey(key); kind {
			case KindURL:
				snap.urls[value] = struct{}{}
			case KindDomain:
				snap.domains[value] = struct{}{}
			}

			return true, nil
		})
	}); err != nil {
		return Snapshot{}, fmt.Errorf("view denylist: %w", err)
	}

	return snap, nil
}

// IsEmpty reports whether the snapshot holds no entries, letting callers skip
// filtering entirely.
func (s Snapshot) IsEmpty() bool {
	return len(s.urls) == 0 && len(s.domains) == 0
}

// Blocks reports whether a raw URL is denylisted: an exact URL match, or a host
// that equals or is a subdomain of a denylisted domain.
func (s Snapshot) Blocks(rawURL string) bool {
	if _, ok := s.urls[rawURL]; ok {
		return true
	}

	host := hostOf(rawURL)
	if host == "" {
		return false
	}
	for {
		if _, ok := s.domains[host]; ok {
			return true
		}
		separator := strings.IndexByte(host, '.')
		if separator < 0 {
			return false
		}
		host = host[separator+1:]
	}
}

func (s *Store) key(kind Kind, value string) vault.Key {
	return vault.Key(string(kind) + keySeparator + value)
}

func splitKey(key vault.Key) (Kind, string) {
	raw := string(key)
	if idx := strings.Index(raw, keySeparator); idx >= 0 {
		return Kind(raw[:idx]), raw[idx+len(keySeparator):]
	}

	return Kind(raw), ""
}

func normalize(kind Kind, value string) string {
	value = strings.TrimSpace(value)
	if kind == KindDomain {
		value = strings.Trim(strings.ToLower(value), ".")
	}

	return value
}

func hostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
}
