package adminauth

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestSessionCapacityPagePreservesOldestExpiryOrdering(t *testing.T) {
	now := time.Unix(1000, 0)
	page := newSessionCapacityPage(now)
	for index := maximumAdminSessions; index > 0; index-- {
		page.observe(
			vault.Key(fmt.Sprintf("active-%06d", index)),
			sessionRecord{ExpiresAt: now.Add(time.Duration(index) * time.Second)},
		)
	}
	remove := page.removals()
	if len(page.active) != maximumAdminSessions || len(remove) != 1 ||
		string(remove[0]) != "active-000001" {
		t.Fatalf("active/removal = %d/%q", len(page.active), remove)
	}
	expired := newSessionCapacityPage(now)
	for index := 0; index < maximumAdminSessions; index++ {
		expired.observe(
			vault.Key(fmt.Sprintf("expired-%06d", index)),
			sessionRecord{ExpiresAt: now},
		)
	}
	if len(expired.removals()) != maximumAdminSessions {
		t.Fatalf("expired candidates = %d", len(expired.expired))
	}
}

func TestSessionStoreCleansLegacyRecordsInBoundedTransactions(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1000, 0)
	engine := &sessionRetentionPagedEngine{scriptedEngine: newScriptedEngine()}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	store, err := newSessionStore(storage, time.Hour, fixedNow(now))
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	legacy := make([]legacySessionFixture, sessionRetentionBatchSize*4)
	for index := range legacy {
		expiresAt := now
		if index >= sessionRetentionBatchSize*2 {
			expiresAt = now.Add(time.Duration(index) * time.Second)
		}
		legacy[index] = legacySessionFixture{
			key:       legacySessionKey(index),
			expiresAt: expiresAt,
		}
	}
	insertLegacySessions(t, store, legacy)
	baseline := len(engine.sessionDeletes)
	if _, err := store.create(ctx, "admin"); err != nil {
		t.Fatalf("create after legacy cleanup: %v", err)
	}
	cleanupDeletes := engine.sessionDeletes[baseline:]
	cleanupScans := engine.sessionScans[baseline:]
	wantDeletes := []int{
		sessionRetentionBatchSize,
		sessionRetentionBatchSize,
		sessionRetentionBatchSize,
		1,
	}
	wantScans := []int{
		sessionRetentionBatchSize,
		sessionRetentionBatchSize,
		sessionRetentionBatchSize,
		sessionRetentionBatchSize,
	}
	if !slices.Equal(cleanupDeletes, wantDeletes) ||
		!slices.Equal(cleanupScans, wantScans) {
		t.Fatalf("cleanup deletes/scans = %v/%v", cleanupDeletes, cleanupScans)
	}
	for update, deleted := range cleanupDeletes {
		if deleted > sessionRetentionBatchSize {
			t.Fatalf("transaction %d deleted %d sessions", update, deleted)
		}
	}
	for update, observed := range cleanupScans {
		if observed > sessionRetentionBatchSize {
			t.Fatalf("transaction %d observed %d sessions", update, observed)
		}
	}
	if length := sessionStoreLength(t, store); length != maximumAdminSessions {
		t.Fatalf("session records = %d, want %d", length, maximumAdminSessions)
	}
	assertSessionRecordPresence(t, store, legacySessionKey(768), false)
	assertSessionRecordPresence(t, store, legacySessionKey(769), true)
}

func TestSessionStoreResumesLegacyCleanupAfterInterruption(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1000, 0)
	engine := &sessionInterruptionEngine{scriptedEngine: newScriptedEngine()}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	store, err := newSessionStore(storage, time.Hour, fixedNow(now))
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	legacy := make([]legacySessionFixture, maximumAdminSessions+sessionRetentionBatchSize)
	for index := range legacy {
		legacy[index] = legacySessionFixture{
			key:       legacySessionKey(index),
			expiresAt: now.Add(time.Minute),
		}
	}
	insertLegacySessions(t, store, legacy)
	engine.interruptAt = engine.updates + 2
	if _, err := store.create(ctx, "admin"); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted create error = %v", err)
	}
	if length := sessionStoreLength(t, store); length != maximumAdminSessions {
		t.Fatalf("partially cleaned records = %d", length)
	}
	engine.interruptAt = 0
	restartedStorage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("restart vault.New: %v", err)
	}
	restarted, err := newSessionStore(restartedStorage, time.Hour, fixedNow(now))
	if err != nil {
		t.Fatalf("restart newSessionStore: %v", err)
	}
	created, err := restarted.create(ctx, "admin")
	if err != nil {
		t.Fatalf("create after restart: %v", err)
	}
	if _, found, err := restarted.lookup(ctx, created.Token); err != nil || !found {
		t.Fatalf("new session lookup = %t, %v", found, err)
	}
	if length := sessionStoreLength(t, restarted); length != maximumAdminSessions {
		t.Fatalf("records after restart = %d", length)
	}
	assertSessionRecordPresence(t, restarted, legacySessionKey(256), false)
	assertSessionRecordPresence(t, restarted, legacySessionKey(257), true)
}

func TestSessionStoreSurfacesLegacyCapacityFaults(t *testing.T) {
	now := time.Unix(1000, 0)
	t.Run("length", func(t *testing.T) {
		store, engine := scriptedSessionStore(t, now)
		engine.buckets[vault.Name("__lengths__")][string(adminSessionsBucket)] = []byte{1}
		if _, err := store.create(context.Background(), "admin"); err == nil {
			t.Fatal("create should surface the length error")
		}
	})
	t.Run("scan", func(t *testing.T) {
		store, engine := scriptedSessionStore(t, now)
		insertLegacySessions(t, store, activeLegacySessions(now, maximumAdminSessions+1))
		engine.buckets[adminSessionsBucket][string(legacySessionKey(0))] = []byte("{")
		if _, err := store.create(context.Background(), "admin"); err == nil {
			t.Fatal("create should surface the legacy scan error")
		}
	})
	t.Run("length mismatch", func(t *testing.T) {
		store, engine := scriptedSessionStore(t, now)
		insertLegacySessions(t, store, activeLegacySessions(now, 1))
		setStoredSessionLength(engine, maximumAdminSessions+1)
		if _, err := store.create(context.Background(), "admin"); err == nil {
			t.Fatal("create should surface the legacy length mismatch")
		}
	})
	t.Run("delete", func(t *testing.T) {
		store, engine := scriptedSessionStore(t, now)
		insertLegacySessions(t, store, activeLegacySessions(now, maximumAdminSessions+1))
		engine.deleteErr = errors.New("locked")
		if _, err := store.create(context.Background(), "admin"); err == nil {
			t.Fatal("create should surface the legacy delete error")
		}
	})
}

func TestSessionStoreConcurrentAdmissionPreservesNewestSessions(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1000, 0)
	store, err := newSessionStore(testVault(t), time.Hour, fixedNow(now))
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	legacy := make([]legacySessionFixture, maximumAdminSessions*3)
	for index := range legacy {
		legacy[index] = legacySessionFixture{
			key:       legacySessionKey(index),
			expiresAt: now.Add(time.Minute),
		}
	}
	insertLegacySessions(t, store, legacy)
	const concurrentAdmissions = 64
	start := make(chan struct{})
	created := make(chan session, concurrentAdmissions)
	errorsFound := make(chan error, concurrentAdmissions)
	var workers sync.WaitGroup
	workers.Add(concurrentAdmissions)
	for range concurrentAdmissions {
		go func() {
			defer workers.Done()
			<-start
			candidate, createErr := store.create(ctx, "admin")
			if createErr != nil {
				errorsFound <- createErr

				return
			}
			created <- candidate
		}()
	}
	close(start)
	workers.Wait()
	close(created)
	close(errorsFound)
	for createErr := range errorsFound {
		t.Errorf("concurrent create: %v", createErr)
	}
	if length := sessionStoreLength(t, store); length != maximumAdminSessions {
		t.Fatalf("concurrent session records = %d", length)
	}
	for candidate := range created {
		if _, found, lookupErr := store.lookup(ctx, candidate.Token); lookupErr != nil || !found {
			t.Errorf("new session lookup = %t, %v", found, lookupErr)
		}
	}
	legacyKeys := storedLegacySessionKeys(t, store)
	if len(legacyKeys) != maximumAdminSessions-concurrentAdmissions ||
		legacyKeys[0] != string(legacySessionKey(576)) ||
		legacyKeys[len(legacyKeys)-1] != string(legacySessionKey(767)) {
		t.Fatalf(
			"retained legacy range = %d, %q..%q",
			len(legacyKeys),
			legacyKeys[0],
			legacyKeys[len(legacyKeys)-1],
		)
	}
}

type legacySessionFixture struct {
	key       vault.Key
	expiresAt time.Time
}

func scriptedSessionStore(
	t *testing.T,
	now time.Time,
) (*sessionStore, *scriptedEngine) {
	t.Helper()
	engine := newScriptedEngine()
	store, err := newSessionStore(scriptedVault(t, engine), time.Hour, fixedNow(now))
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}

	return store, engine
}

func activeLegacySessions(now time.Time, length int) []legacySessionFixture {
	fixtures := make([]legacySessionFixture, length)
	for index := range fixtures {
		fixtures[index] = legacySessionFixture{
			key:       legacySessionKey(index),
			expiresAt: now.Add(time.Minute),
		}
	}

	return fixtures
}

func setStoredSessionLength(engine *scriptedEngine, length uint64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], length)
	engine.buckets[vault.Name("__lengths__")][string(adminSessionsBucket)] = raw[:]
}

func legacySessionKey(index int) vault.Key {
	return vault.Key(fmt.Sprintf("legacy-%06d", index))
}

func insertLegacySessions(
	t *testing.T,
	store *sessionStore,
	fixtures []legacySessionFixture,
) {
	t.Helper()
	if err := store.vault.Update(context.Background(), func(tx *vault.Txn) error {
		for _, fixture := range fixtures {
			if err := store.records.Put(tx, fixture.key, sessionRecord{
				Username:  "admin",
				CSRFToken: string(fixture.key),
				ExpiresAt: fixture.expiresAt,
			}); err != nil {
				return fmt.Errorf("store legacy session: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("insert legacy sessions: %v", err)
	}
}

func assertSessionRecordPresence(
	t *testing.T,
	store *sessionStore,
	key vault.Key,
	want bool,
) {
	t.Helper()
	found := false
	if err := store.vault.View(context.Background(), func(tx *vault.Txn) error {
		var getErr error
		_, found, getErr = store.records.Get(tx, key)
		if getErr != nil {
			return fmt.Errorf("get session record: %w", getErr)
		}

		return nil
	}); err != nil {
		t.Fatalf("read session record: %v", err)
	}
	if found != want {
		t.Fatalf("session %q presence = %t, want %t", key, found, want)
	}
}

func storedLegacySessionKeys(t *testing.T, store *sessionStore) []string {
	t.Helper()
	var keys []string
	if err := store.vault.View(context.Background(), func(tx *vault.Txn) error {
		return store.records.Scan(
			tx,
			vault.Key("legacy-"),
			func(key vault.Key, _ sessionRecord) (bool, error) {
				keys = append(keys, string(key))

				return true, nil
			},
		)
	}); err != nil {
		t.Fatalf("scan legacy sessions: %v", err)
	}
	slices.Sort(keys)

	return keys
}

type sessionRetentionPagedEngine struct {
	*scriptedEngine
	sessionDeletes []int
	sessionScans   []int
}

func (e *sessionRetentionPagedEngine) Update(
	_ context.Context,
	update func(vault.EngineTxn) error,
) error {
	deleted := 0
	observed := 0
	err := update(sessionRetentionPagedTxn{
		base:     scriptedTxn{engine: e.scriptedEngine, writable: true},
		deleted:  &deleted,
		observed: &observed,
	})
	e.sessionDeletes = append(e.sessionDeletes, deleted)
	e.sessionScans = append(e.sessionScans, observed)

	return err
}

type sessionRetentionPagedTxn struct {
	base     scriptedTxn
	deleted  *int
	observed *int
}

func (t sessionRetentionPagedTxn) Writable() bool {
	return true
}

func (t sessionRetentionPagedTxn) Bucket(name vault.Name) vault.EngineBucket {
	bucket := t.base.Bucket(name)
	if name != adminSessionsBucket {
		return bucket
	}

	return sessionRetentionPagedBucket{
		EngineBucket: bucket,
		deleted:      t.deleted,
		observed:     t.observed,
	}
}

type sessionRetentionPagedBucket struct {
	vault.EngineBucket
	deleted  *int
	observed *int
}

func (b sessionRetentionPagedBucket) Delete(key vault.Key) error {
	if err := b.EngineBucket.Delete(key); err != nil {
		return fmt.Errorf("paged session deletion: %w", err)
	}
	*b.deleted++

	return nil
}

func (b sessionRetentionPagedBucket) Scan(
	prefix vault.Key,
	observe func(vault.Key, []byte) (bool, error),
) error {
	if err := b.EngineBucket.Scan(prefix, func(key vault.Key, raw []byte) (bool, error) {
		*b.observed++

		return observe(key, raw)
	}); err != nil {
		return fmt.Errorf("paged session scan: %w", err)
	}

	return nil
}

type sessionInterruptionEngine struct {
	*scriptedEngine
	updates     int
	interruptAt int
}

func (e *sessionInterruptionEngine) Update(
	ctx context.Context,
	update func(vault.EngineTxn) error,
) error {
	e.updates++
	if e.interruptAt > 0 && e.updates == e.interruptAt {
		return context.Canceled
	}

	return e.scriptedEngine.Update(ctx, update)
}
