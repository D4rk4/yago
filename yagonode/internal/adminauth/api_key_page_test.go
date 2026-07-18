package adminauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestLegacyAPIKeyPagesAreBoundedCompleteAndDisjoint(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	want := storeLegacyAPIKeyRecords(t, store, maximumAPIKeys+44)
	sort.Strings(want)

	seen := make([]string, 0, len(want))
	cursor := ""
	for {
		page, err := store.page(context.Background(), cursor, 20)
		if err != nil {
			t.Fatalf("page after %q: %v", cursor, err)
		}
		if len(page.infos) == 0 || len(page.infos) > 20 || page.total != len(want) {
			t.Fatalf("page after %q = %+v", cursor, page)
		}
		for _, info := range page.infos {
			seen = append(seen, info.ID)
		}
		if page.nextCursor == "" {
			break
		}
		if page.nextCursor != page.infos[len(page.infos)-1].ID {
			t.Fatalf("next cursor %q does not match the page boundary", page.nextCursor)
		}
		cursor = page.nextCursor
	}
	if len(seen) != len(want) {
		t.Fatalf("listed %d keys, want %d", len(seen), len(want))
	}
	for index := range want {
		if seen[index] != want[index] {
			t.Fatalf("key %d = %q, want %q", index, seen[index], want[index])
		}
	}
	if _, err := store.list(context.Background()); !errors.Is(
		err,
		errAPIKeyCompatibilityListingTruncated,
	) {
		t.Fatalf("compatibility list error = %v", err)
	}
}

func TestAPIKeyPageRejectsInvalidCursorAndLimits(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	if _, err := store.page(context.Background(), "bad", 20); !errors.Is(
		err,
		errInvalidAPIKeyPageCursor,
	) {
		t.Fatalf("invalid cursor error = %v", err)
	}
	for _, limit := range []int{0, maximumAPIKeys + 1} {
		if _, err := store.page(context.Background(), "", limit); !errors.Is(
			err,
			errInvalidAPIKeyPageLimit,
		) {
			t.Fatalf("limit %d error = %v", limit, err)
		}
	}
}

func TestAPIKeyPageSurfacesMalformedRecordWithinPage(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	ids := storeLegacyAPIKeyRecords(t, store, 2)
	sort.Strings(ids)
	engine.buckets[adminAPIKeysBucket][ids[1]] = []byte("not-json")
	if _, err := store.page(context.Background(), "", 20); err == nil {
		t.Fatal("malformed page record was accepted")
	}
}

func TestAPIKeyPageDoesNotDecodeBeyondRequestedBoundary(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	ids := storeLegacyAPIKeyRecords(t, store, 21)
	sort.Strings(ids)
	engine.buckets[adminAPIKeysBucket][ids[20]] = []byte("not-json")
	first, err := store.page(context.Background(), "", 20)
	if err != nil || len(first.infos) != 20 || first.nextCursor == "" {
		t.Fatalf("bounded first page = %+v, %v", first, err)
	}
	if _, err := store.page(context.Background(), first.nextCursor, 20); err == nil {
		t.Fatal("malformed record was accepted when it entered the requested page")
	}
}

func TestAPIKeyPageSurfacesMalformedIdentifierWithinPage(t *testing.T) {
	store, _, _ := newTestKeyStore(t)
	if err := store.vault.Update(context.Background(), func(tx *vault.Txn) error {
		return store.records.Put(tx, vault.Key("bad"), apiKeyRecord{})
	}); err != nil {
		t.Fatalf("store malformed identifier: %v", err)
	}
	if _, err := store.page(context.Background(), "", 20); err == nil {
		t.Fatal("malformed page identifier was accepted")
	}
}

func TestAPIKeyPageSurfacesLengthAndReaderErrors(t *testing.T) {
	store, engine, _ := newTestKeyStore(t)
	engine.buckets[vault.Name("__lengths__")][string(adminAPIKeysBucket)] = []byte{1}
	if _, err := store.page(context.Background(), "", 20); err == nil {
		t.Fatal("malformed length was accepted by page")
	}
	if _, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead}); err == nil {
		t.Fatal("malformed length was accepted by create")
	}

	engine.buckets[vault.Name("__lengths__")][string(adminAPIKeysBucket)] = make([]byte, 8)
	engine.pageErr = errors.New("page unavailable")
	if _, err := store.page(context.Background(), "", 20); err == nil {
		t.Fatal("reader error was not surfaced")
	}
}

func TestAPIKeyStoreSkipsFreshLastUsedRewrite(t *testing.T) {
	store, engine, clock := newTestKeyStore(t)
	created, err := store.create(context.Background(), "ci", []Scope{ScopeAdminRead})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.touchLastUsed(context.Background(), created.ID); err != nil {
		t.Fatalf("initial touch: %v", err)
	}
	clock.now = clock.now.Add(time.Second)
	engine.putErr = errors.New("unexpected rewrite")
	if found, err := store.touchLastUsed(context.Background(), created.ID); err != nil || !found {
		t.Fatalf("fresh touch = %v, %v", found, err)
	}
}

func storeLegacyAPIKeyRecords(
	t *testing.T,
	store *apiKeyStore,
	total int,
) []string {
	t.Helper()
	ids := make([]string, 0, total)
	err := store.vault.Update(context.Background(), func(tx *vault.Txn) error {
		for index := range total {
			id := deterministicAPIKeyID(index)
			record := apiKeyRecord{
				SecretHash: fmt.Sprintf("hash-%d", index),
				Scopes:     []Scope{ScopeAdminRead},
				Label:      fmt.Sprintf("legacy-%d", index),
				CreatedAt:  time.Unix(int64(total-index), 0),
			}
			if err := store.records.Put(tx, vault.Key(id), record); err != nil {
				return fmt.Errorf("store legacy API key %d: %w", index, err)
			}
			ids = append(ids, id)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("store legacy API keys: %v", err)
	}

	return ids
}

func deterministicAPIKeyID(index int) string {
	raw := sha256.Sum256([]byte(fmt.Sprintf("legacy-api-key-%d", index)))

	return base64.RawURLEncoding.EncodeToString(raw[:apiKeyIDBytes])
}
