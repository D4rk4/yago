package adminauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func unknownAPIKey(index int) string {
	identity := sha256.Sum256([]byte(strconv.Itoa(index)))
	id := base64.RawURLEncoding.EncodeToString(identity[:apiKeyIDBytes])
	secret := base64.RawURLEncoding.EncodeToString(make([]byte, apiKeySecretBytes))

	return formatAPIKey(id, secret)
}

func TestParseAPIKeyRejectsInvalidBase64URL(t *testing.T) {
	valid := unknownAPIKey(1)
	invalidID := valid[:len(apiKeyScheme)] + "!" + valid[len(apiKeyScheme)+1:]
	invalidSecret := valid[:len(valid)-1] + "!"
	for _, presented := range []string{invalidID, invalidSecret} {
		if _, _, ok := parseAPIKey(presented); ok {
			t.Fatalf("invalid API key parsed: %q", presented)
		}
	}
}

func TestAPIKeyRateLimiterCapsKeysAndReclaimsStaleEntries(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	limiter := newAPIKeyRateLimiter(2, time.Minute, clock.Now)
	for index := range maximumTrackedAPIKeys {
		if !limiter.allow(fmt.Sprintf("key-%d", index)) {
			t.Fatalf("key %d denied before capacity", index)
		}
	}
	if limiter.allow("overflow") || len(limiter.events) != maximumTrackedAPIKeys {
		t.Fatalf("full key limiter = %d", len(limiter.events))
	}
	if !limiter.allow("key-0") || limiter.allow("key-0") {
		t.Fatal("tracked key did not retain its per-window budget")
	}

	clock.now = clock.now.Add(time.Minute)
	if !limiter.allow("fresh") || len(limiter.events) != 1 {
		t.Fatalf("stale key reclamation retained %d keys", len(limiter.events))
	}
}

func TestAPIKeyRateLimiterEvictsOnlyStaleLRUPrefix(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	limiter := newAPIKeyRateLimiter(3, time.Minute, clock.Now)
	limiter.allow("a")
	limiter.allow("b")
	clock.now = clock.now.Add(30 * time.Second)
	limiter.allow("a")
	clock.now = clock.now.Add(31 * time.Second)
	limiter.allow("c")
	if _, found := limiter.events["b"]; found {
		t.Fatal("stale least-recent key was retained")
	}
	if len(limiter.events) != 2 || limiter.order.Front().Value.(string) != "a" {
		t.Fatalf("LRU state = %d/%v", len(limiter.events), limiter.order.Front().Value)
	}
}

func TestAPIKeyRateLimiterBoundsEventsAndOwnsKeys(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	minimum := newAPIKeyRateLimiter(0, time.Minute, clock.Now)
	if !minimum.allow("minimum") || minimum.allow("minimum") || minimum.max != 1 {
		t.Fatalf("minimum limiter max = %d", minimum.max)
	}
	maximum := newAPIKeyRateLimiter(maximumAPIKeyEvents+1, time.Minute, clock.Now)
	if maximum.max != maximumAPIKeyEvents {
		t.Fatalf("maximum events = %d", maximum.max)
	}

	backing := strings.Repeat("x", 1<<20)
	key := backing[100:110]
	maximum.allow(key)
	for retained := range maximum.events {
		backingStart := uintptr(reflect.ValueOf(backing).UnsafePointer())
		backingEnd := backingStart + uintptr(len(backing))
		retainedStart := uintptr(reflect.ValueOf(retained).UnsafePointer())
		if retainedStart >= backingStart && retainedStart < backingEnd {
			t.Fatal("API key limiter retained source storage")
		}
	}
}

func TestAPIKeyRateLimiterConcurrentCapacityInvariant(t *testing.T) {
	limiter := newAPIKeyRateLimiter(5, time.Minute, time.Now)
	var next atomic.Int64
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				index := int(next.Add(1)) - 1
				if index >= maximumTrackedAPIKeys+512 {
					return
				}
				limiter.allow(fmt.Sprintf("key-%d", index))
			}
		}()
	}
	workers.Wait()
	if len(limiter.events) != maximumTrackedAPIKeys ||
		limiter.order.Len() != maximumTrackedAPIKeys || limiter.allow("overflow") {
		t.Fatalf("concurrent key capacity = %d/%d", len(limiter.events), limiter.order.Len())
	}
}

func TestAPIKeyRateLimiterConcurrentEventInvariant(t *testing.T) {
	limiter := newAPIKeyRateLimiter(5, time.Minute, time.Now)
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				limiter.allow("shared")
			}
		}()
	}
	workers.Wait()
	if len(limiter.events["shared"].stamps) != limiter.max || limiter.order.Len() != 1 {
		t.Fatalf("concurrent events = %d/%d",
			len(limiter.events["shared"].stamps), limiter.order.Len())
	}
}

func TestAPIKeyAuthorizerBoundsUnknownIdentifiers(t *testing.T) {
	service, err := New(testVault(t), Config{
		APIKeyMaxPerWindow: 2,
		APIKeyWindow:       time.Minute,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	authorizer := service.APIKeyAuthorizer()
	for index := range maximumTrackedAPIKeys + 512 {
		if outcome := authorizer.Authorize(
			t.Context(),
			unknownAPIKey(index),
			ScopeAdminRead,
		); outcome != APIKeyUnauthenticated {
			t.Fatalf("unknown key %d outcome = %v", index, outcome)
		}
	}
	if len(service.keyLimiter.events) != 0 {
		t.Fatalf("unknown keys retained %d limiter entries", len(service.keyLimiter.events))
	}
	real := createKey(t, service, ScopeAdminRead)
	if outcome := authorizer.Authorize(
		t.Context(), real.Key, ScopeAdminRead,
	); outcome != APIKeyAuthorized || len(service.keyLimiter.events) != 1 {
		t.Fatalf("real key outcome = %v/%d", outcome, len(service.keyLimiter.events))
	}
}

func TestAPIKeyAuthorizerRejectsMalformedIdentifierBeforeLimiter(t *testing.T) {
	service := testService(t)
	token := unknownAPIKey(1)
	token = token[:len(apiKeyScheme)] + "!" + token[len(apiKeyScheme)+1:]
	if outcome := service.APIKeyAuthorizer().Authorize(
		t.Context(),
		token,
		ScopeAdminRead,
	); outcome != APIKeyUnauthenticated || len(service.keyLimiter.events) != 0 {
		t.Fatalf("malformed key outcome = %v/%d", outcome, len(service.keyLimiter.events))
	}
}

func TestAPIKeyAuthenticationAdmissionRejectsConcurrentOverflow(t *testing.T) {
	releases := make([]func(), 0, maximumConcurrentAPIKeyAuthentications)
	for range maximumConcurrentAPIKeyAuthentications {
		release, admitted := acquireAPIKeyAuthentication()
		if !admitted {
			t.Fatal("authentication admission filled before its capacity")
		}
		releases = append(releases, release)
	}
	t.Cleanup(func() {
		for _, release := range releases {
			release()
		}
	})

	const callers = 64
	results := make(chan bool, callers)
	var workers sync.WaitGroup
	for range callers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			release, admitted := acquireAPIKeyAuthentication()
			if admitted {
				release()
			}
			results <- admitted
		}()
	}
	workers.Wait()
	close(results)
	for admitted := range results {
		if admitted {
			t.Fatal("authentication overflow was admitted")
		}
	}
}

func TestAPIKeyAuthorizerRejectsWhenAuthenticationAdmissionIsFull(t *testing.T) {
	releases := make([]func(), 0, maximumConcurrentAPIKeyAuthentications)
	for range maximumConcurrentAPIKeyAuthentications {
		release, admitted := acquireAPIKeyAuthentication()
		if !admitted {
			t.Fatal("authentication admission filled before its capacity")
		}
		releases = append(releases, release)
	}
	t.Cleanup(func() {
		for _, release := range releases {
			release()
		}
	})

	service := testService(t)
	created := createKey(t, service, ScopeAdminRead)
	if outcome := service.APIKeyAuthorizer().Authorize(
		context.Background(), created.Key, ScopeAdminRead,
	); outcome != APIKeyUnavailable {
		t.Fatalf("outcome = %v, want unavailable", outcome)
	}
	infos, err := service.apiKeys.list(context.Background())
	if err != nil || len(infos) != 1 || !infos[0].LastUsedAt.IsZero() {
		t.Fatalf("keys after rejection = %#v, %v", infos, err)
	}
	if len(service.keyLimiter.events) != 0 {
		t.Fatalf("rate limiter retained %d keys", len(service.keyLimiter.events))
	}
}
