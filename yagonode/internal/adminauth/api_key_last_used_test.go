package adminauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestAPIKeyAuthorizerCoalescesLastUsedWrites(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	service, err := New(testVault(t), Config{Now: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	created, err := service.apiKeys.create(
		context.Background(),
		"search",
		[]Scope{ScopeSearchRead},
	)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	authorizer := service.APIKeyAuthorizer()
	clock.now = time.Unix(2000, 0)
	if outcome := authorizer.Authorize(
		context.Background(), created.Key, ScopeSearchRead,
	); outcome != APIKeyAuthorized {
		t.Fatalf("first outcome = %v", outcome)
	}
	firstUse := apiKeyLastUsedAt(t, service.apiKeys)
	clock.now = clock.now.Add(apiKeyLastUsedRefreshInterval - time.Second)
	if outcome := authorizer.Authorize(
		context.Background(), created.Key, ScopeSearchRead,
	); outcome != APIKeyAuthorized {
		t.Fatalf("coalesced outcome = %v", outcome)
	}
	if got := apiKeyLastUsedAt(t, service.apiKeys); !got.Equal(firstUse) {
		t.Fatalf("coalesced last use = %v, want %v", got, firstUse)
	}
	clock.now = clock.now.Add(time.Second)
	if outcome := authorizer.Authorize(
		context.Background(), created.Key, ScopeSearchRead,
	); outcome != APIKeyAuthorized {
		t.Fatalf("refresh outcome = %v", outcome)
	}
	if got := apiKeyLastUsedAt(t, service.apiKeys); !got.Equal(clock.now) {
		t.Fatalf("refreshed last use = %v, want %v", got, clock.now)
	}
}

func TestAPIKeyAuthorizerTreatsLastUsedFailureAsBestEffort(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	service, engine := scriptedService(t)
	created := createKey(t, service, ScopeSearchRead)
	_, secret, ok := parseAPIKey(created.Key)
	if !ok {
		t.Fatal("created key did not parse")
	}
	engine.putErr = errors.New("disk full")
	for range 2 {
		if outcome := service.APIKeyAuthorizer().Authorize(
			context.Background(), created.Key, ScopeSearchRead,
		); outcome != APIKeyAuthorized {
			t.Fatalf("outcome = %v, want authorized", outcome)
		}
	}
	logs := output.String()
	if strings.Count(logs, lastUsedUpdateFailedEvent) != 1 {
		t.Fatalf("warning count in %q", logs)
	}
	if strings.Contains(logs, created.Key) || strings.Contains(logs, secret) {
		t.Fatalf("warning exposed key material: %q", logs)
	}
}

func TestAPIKeyDeletionForgetsLastUsedAdmission(t *testing.T) {
	service := testService(t)
	created := createKey(t, service, ScopeSearchRead)
	if outcome := service.APIKeyAuthorizer().Authorize(
		context.Background(), created.Key, ScopeSearchRead,
	); outcome != APIKeyAuthorized {
		t.Fatalf("outcome = %v", outcome)
	}
	if _, err := service.apiKeys.delete(context.Background(), created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	service.apiKeys.lastUsedRecorder.mu.Lock()
	_, retained := service.apiKeys.lastUsedRecorder.nextAttempt[created.ID]
	service.apiKeys.lastUsedRecorder.mu.Unlock()
	if retained {
		t.Fatal("deleted key retained last-used admission")
	}
}

type pausedLastUsedEngine struct {
	*scriptedEngine
	updates      atomic.Int64
	touchStarted chan struct{}
	releaseTouch chan struct{}
}

func (engine *pausedLastUsedEngine) Update(
	ctx context.Context,
	update func(vault.EngineTxn) error,
) error {
	if engine.updates.Add(1) == 2 {
		close(engine.touchStarted)
		select {
		case <-ctx.Done():
			return fmt.Errorf("pause last-used update: %w", ctx.Err())
		case <-engine.releaseTouch:
		}
	}

	return engine.scriptedEngine.Update(ctx, update)
}

func TestConcurrentAPIKeyRevocationDoesNotRetainLastUsedAdmission(t *testing.T) {
	engine := &pausedLastUsedEngine{
		scriptedEngine: newScriptedEngine(),
		touchStarted:   make(chan struct{}),
		releaseTouch:   make(chan struct{}),
	}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	service, err := New(storage, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	created := createKey(t, service, ScopeSearchRead)
	outcome := make(chan APIKeyOutcome, 1)
	go func() {
		outcome <- service.APIKeyAuthorizer().Authorize(
			t.Context(),
			created.Key,
			ScopeSearchRead,
		)
	}()
	<-engine.touchStarted
	if deleted, deleteErr := service.apiKeys.delete(
		t.Context(),
		created.ID,
	); deleteErr != nil || !deleted {
		t.Fatalf("delete during last-used update = %t, %v", deleted, deleteErr)
	}
	close(engine.releaseTouch)
	if got := <-outcome; got != APIKeyAuthorized {
		t.Fatalf("authorization outcome = %v, want authorized", got)
	}
	service.apiKeys.lastUsedRecorder.mu.Lock()
	retained := len(service.apiKeys.lastUsedRecorder.nextAttempt)
	service.apiKeys.lastUsedRecorder.mu.Unlock()
	if retained != 0 {
		t.Fatalf("revoked key retained %d last-used admissions", retained)
	}
}

func TestLegacyAPIKeysBeyondRecorderCapacityRemainAuthorized(t *testing.T) {
	ctx := context.Background()
	clock := &mutableClock{now: time.Unix(1000, 0)}
	service, err := New(testVault(t), Config{Now: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	presented := make([]string, maximumAPIKeys+1)
	identifiers := make([]string, len(presented))
	if err := service.apiKeys.vault.Update(ctx, func(tx *vault.Txn) error {
		for index := range presented {
			id, key := deterministicLegacyAPIKey(index)
			identifiers[index] = id
			presented[index] = key
			_, secret, _ := parseAPIKey(key)
			if putErr := service.apiKeys.records.Put(tx, vault.Key(id), apiKeyRecord{
				SecretHash: hashToken(secret),
				Scopes:     []Scope{ScopeSearchRead},
				CreatedAt:  clock.now,
			}); putErr != nil {
				return fmt.Errorf("store legacy API key: %w", putErr)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("store legacy keys: %v", err)
	}
	authorizer := service.APIKeyAuthorizer()
	for index := 0; index < maximumAPIKeys; index++ {
		if outcome := authorizer.Authorize(
			ctx,
			presented[index],
			ScopeSearchRead,
		); outcome != APIKeyAuthorized {
			t.Fatalf("legacy key %d outcome = %v", index, outcome)
		}
	}
	if outcome := authorizer.Authorize(
		ctx,
		presented[maximumAPIKeys],
		ScopeSearchRead,
	); outcome != APIKeyAuthorized {
		t.Fatalf("over-cap legacy key outcome = %v", outcome)
	}
	service.apiKeys.lastUsedRecorder.mu.Lock()
	tracked := len(service.apiKeys.lastUsedRecorder.nextAttempt)
	_, overflowTracked := service.apiKeys.lastUsedRecorder.nextAttempt[identifiers[maximumAPIKeys]]
	service.apiKeys.lastUsedRecorder.mu.Unlock()
	if tracked != maximumAPIKeys || overflowTracked {
		t.Fatalf("last-used admissions = %d, overflow tracked = %t", tracked, overflowTracked)
	}
	if lastUsed := storedAPIKeyLastUsed(
		t,
		service.apiKeys,
		identifiers[maximumAPIKeys],
	); !lastUsed.IsZero() {
		t.Fatalf("over-cap LastUsedAt = %v, want zero", lastUsed)
	}
	clock.now = clock.now.Add(apiKeyLastUsedRefreshInterval)
	if outcome := authorizer.Authorize(
		ctx,
		presented[0],
		ScopeSearchRead,
	); outcome != APIKeyAuthorized {
		t.Fatalf("tracked legacy key refresh outcome = %v", outcome)
	}
	if lastUsed := storedAPIKeyLastUsed(
		t,
		service.apiKeys,
		identifiers[0],
	); !lastUsed.Equal(clock.now) {
		t.Fatalf("tracked LastUsedAt = %v, want %v", lastUsed, clock.now)
	}
	service.apiKeys.lastUsedRecorder.mu.Lock()
	tracked = len(service.apiKeys.lastUsedRecorder.nextAttempt)
	service.apiKeys.lastUsedRecorder.mu.Unlock()
	if tracked != maximumAPIKeys {
		t.Fatalf("last-used admissions after refresh = %d", tracked)
	}
}

func deterministicLegacyAPIKey(index int) (string, string) {
	secretBytes := sha256.Sum256([]byte(strconv.Itoa(index)))
	id := base64.RawURLEncoding.EncodeToString(secretBytes[:apiKeyIDBytes])
	secret := base64.RawURLEncoding.EncodeToString(secretBytes[:])

	return id, formatAPIKey(id, secret)
}

func storedAPIKeyLastUsed(t *testing.T, store *apiKeyStore, id string) time.Time {
	t.Helper()
	var record apiKeyRecord
	if err := store.vault.View(context.Background(), func(tx *vault.Txn) error {
		var found bool
		var getErr error
		record, found, getErr = store.records.Get(tx, vault.Key(id))
		if getErr != nil {
			return fmt.Errorf("get stored API key: %w", getErr)
		}
		if !found {
			return errors.New("stored API key is missing")
		}

		return nil
	}); err != nil {
		t.Fatalf("read stored API key: %v", err)
	}

	return record.LastUsedAt
}

func apiKeyLastUsedAt(t *testing.T, store *apiKeyStore) time.Time {
	t.Helper()
	infos, err := store.list(context.Background())
	if err != nil || len(infos) != 1 {
		t.Fatalf("list = %#v, %v", infos, err)
	}

	return infos[0].LastUsedAt
}
