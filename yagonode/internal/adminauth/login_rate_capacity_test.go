package adminauth

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginRateLimiterCapsClientsAndEvictsStaleEntries(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	limiter := newLoginRateLimiter(2, time.Minute, clock.Now)
	for index := range maximumTrackedLoginClients {
		limiter.recordFailure(fmt.Sprintf("client-%d", index))
	}
	if len(limiter.failures) != maximumTrackedLoginClients || limiter.allow("overflow") {
		t.Fatalf("full limiter = %d clients", len(limiter.failures))
	}
	limiter.recordFailure("overflow")
	if len(limiter.failures) != maximumTrackedLoginClients {
		t.Fatalf("overflow grew limiter to %d", len(limiter.failures))
	}
	if !limiter.allow("client-0") {
		t.Fatal("tracked client was denied below its failure limit")
	}

	clock.now = clock.now.Add(time.Minute)
	if !limiter.allow("fresh") {
		t.Fatal("fresh client was denied after stale eviction")
	}
	limiter.recordFailure("fresh")
	if len(limiter.failures) != 1 {
		t.Fatalf("stale entries retained = %d", len(limiter.failures))
	}
}

func TestLoginRateLimiterBoundsFailuresAndOwnsKeys(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1000, 0)}
	minimum := newLoginRateLimiter(0, time.Minute, clock.Now)
	minimum.recordFailure("client")
	minimum.recordFailure("client")
	if minimum.max != 1 || len(minimum.failures["client"]) != 1 || minimum.allow("client") {
		t.Fatalf("minimum limiter = %d/%d", minimum.max, len(minimum.failures["client"]))
	}
	maximum := newLoginRateLimiter(maximumLoginFailuresPerClient+1, time.Minute, clock.Now)
	if maximum.max != maximumLoginFailuresPerClient {
		t.Fatalf("maximum failures = %d", maximum.max)
	}

	backing := strings.Repeat("x", 1<<20)
	key := backing[100:110]
	maximum.recordFailure(key)
	for retained := range maximum.failures {
		backingStart := uintptr(reflect.ValueOf(backing).UnsafePointer())
		backingEnd := backingStart + uintptr(len(backing))
		retainedStart := uintptr(reflect.ValueOf(retained).UnsafePointer())
		if retainedStart >= backingStart && retainedStart < backingEnd {
			t.Fatal("limiter key retained source storage")
		}
	}
}

func TestLoginRateLimiterConcurrentClientCapacityInvariant(t *testing.T) {
	limiter := newLoginRateLimiter(5, time.Minute, time.Now)
	var next atomic.Int64
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				index := int(next.Add(1)) - 1
				if index >= maximumTrackedLoginClients+512 {
					return
				}
				key := fmt.Sprintf("client-%d", index)
				if limiter.allow(key) {
					limiter.recordFailure(key)
				}
			}
		}()
	}
	workers.Wait()
	if len(limiter.failures) > maximumTrackedLoginClients || limiter.allow("overflow") {
		t.Fatalf("concurrent clients = %d", len(limiter.failures))
	}
}

func TestLoginRateLimiterConcurrentFailureInvariant(t *testing.T) {
	limiter := newLoginRateLimiter(5, time.Minute, time.Now)
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				limiter.recordFailure("shared")
				_ = limiter.allow("shared")
			}
		}()
	}
	workers.Wait()
	if len(limiter.failures["shared"]) != limiter.max {
		t.Fatalf("concurrent failures = %d", len(limiter.failures["shared"]))
	}
}

func TestLoginRateLimiterConcurrentResetInvariant(t *testing.T) {
	limiter := newLoginRateLimiter(5, time.Minute, time.Now)
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				limiter.recordFailure("shared")
				limiter.reset("shared")
			}
		}()
	}
	workers.Wait()
	if len(limiter.failures) > 1 {
		t.Fatalf("concurrent reset retained %d clients", len(limiter.failures))
	}
}
