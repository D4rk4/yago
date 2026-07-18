package crawllease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestGrantRegistryConfirmsOnlyExplicitLiveRenewals(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	registry := NewGrantRegistry(ctx, 2)
	if err := registry.Track("lease-a"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Track("lease-b"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Track("lease-c"); err == nil {
		t.Fatal("capacity overflow was accepted")
	}
	started := time.Now()
	registry.Renew(started, time.Second, []string{"lease-a", "lease-b"}, []string{"lease-a"})
	if !registry.Confirmed("lease-a") {
		t.Fatal("explicit renewal was not confirmed")
	}
	if registry.Confirmed("lease-b") {
		t.Fatal("omitted renewal remained live")
	}
	if active := registry.ActiveLeaseIDs(); len(active) != 1 || active[0] != "lease-a" {
		t.Fatalf("active leases = %v", active)
	}
}

func TestGrantRegistryCapacityIsIndependentFromFetchWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	registry := NewGrantRegistry(ctx, yagocrawlcontract.MaximumHeartbeatActiveLeases)
	tracked := yagocrawlcontract.MaximumFetchWorkerConcurrency + 1
	for index := range tracked {
		if err := registry.Track(fmt.Sprintf("lease-%03d", index)); err != nil {
			t.Fatalf("track lease %d of %d: %v", index+1, tracked, err)
		}
	}
	if active := registry.ActiveLeaseIDs(); len(active) != tracked {
		t.Fatalf("active leases = %d, want %d", len(active), tracked)
	}
}

func TestGrantRegistryNeverResurrectsExpiredGrant(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	now := time.Now()
	registry := newGrantRegistry(ctx, 1, func() time.Time { return now }, false)
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	registry.Renew(now, time.Second, []string{"lease"}, []string{"lease"})
	leaseContext, ok := registry.Context("lease")
	if !ok {
		t.Fatal("initial grant was not confirmed")
	}
	now = now.Add(time.Second)
	registry.Renew(now, time.Second, []string{"lease"}, []string{"lease"})
	if registry.Confirmed("lease") {
		t.Fatal("expired grant was resurrected")
	}
	if !errors.Is(context.Cause(leaseContext), ErrLeaseLost) {
		t.Fatalf("lease context cause = %v", context.Cause(leaseContext))
	}
}

func TestGrantRegistryUsesRequestStartForDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := time.Now()
	now := started.Add(900 * time.Millisecond)
	registry := newGrantRegistry(ctx, 1, func() time.Time { return now }, false)
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	registry.Renew(started, time.Second, []string{"lease"}, []string{"lease"})
	if !registry.Confirmed("lease") {
		t.Fatal("grant with remaining conservative lifetime was rejected")
	}
	now = started.Add(time.Second)
	if registry.Confirmed("lease") {
		t.Fatal("response receipt incorrectly extended the deadline")
	}
}

func TestGrantRegistryCancellationAndIdentityContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	registry := NewGrantRegistry(ctx, 1)
	if err := registry.Track(""); err == nil {
		t.Fatal("empty lease id was accepted")
	}
	if err := registry.Track(strings.Repeat(
		"l",
		yagocrawlcontract.MaximumCrawlLeaseIDBytes+1,
	)); err == nil {
		t.Fatal("oversized lease id was accepted")
	}
	if err := registry.Track(string([]byte{0xff})); err == nil {
		t.Fatal("invalid lease id encoding was accepted")
	}
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	registry.Renew(started, time.Hour, []string{"lease"}, []string{"lease"})
	leaseContext, ok := registry.Context("lease")
	if !ok {
		t.Fatal("lease context missing")
	}
	registry.Revoke("missing")
	cancel()
	select {
	case <-leaseContext.Done():
	case <-time.After(time.Second):
		t.Fatal("registry shutdown did not cancel the grant")
	}
	identityContext := WithLeaseID(t.Context(), "lease")
	if got := LeaseID(identityContext); got != "lease" || LeaseID(t.Context()) != "" {
		t.Fatalf("lease identity = %q", got)
	}
}

func TestGrantRegistryIgnoresOlderHeartbeatResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	now := time.Unix(10_000, 0)
	registry := newGrantRegistry(ctx, 1, func() time.Time { return now }, false)
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	newerRequest := now.Add(time.Second)
	registry.Renew(newerRequest, time.Minute, []string{"lease"}, []string{"lease"})
	registry.Renew(now, time.Second, []string{"lease"}, nil)
	now = newerRequest.Add(30 * time.Second)
	if !registry.Confirmed("lease") {
		t.Fatal("older omitted heartbeat response revoked a newer renewal")
	}
}

func TestGrantRegistryWatchdogCancelsExpiredGrant(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	registry := NewGrantRegistry(ctx, 1)
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	registry.Renew(started, 25*time.Millisecond, []string{"lease"}, []string{"lease"})
	leaseContext, ok := registry.Context("lease")
	if !ok {
		t.Fatal("confirmed grant context missing")
	}
	select {
	case <-leaseContext.Done():
	case <-time.After(time.Second):
		t.Fatal("watchdog did not cancel expired grant")
	}
	if !errors.Is(context.Cause(leaseContext), ErrLeaseLost) {
		t.Fatalf("lease context cause = %v", context.Cause(leaseContext))
	}
}

func TestGrantRegistryContextRequiresATrackedConfirmedGrant(t *testing.T) {
	now := time.Unix(20_000, 0)
	registry := newGrantRegistry(t.Context(), 1, func() time.Time { return now }, false)
	if _, ok := registry.Context("missing"); ok {
		t.Fatal("missing lease exposed a context")
	}
	if err := registry.Track("lease"); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Context("lease"); ok {
		t.Fatal("unconfirmed lease exposed a context")
	}
	registry.Renew(
		now,
		time.Minute,
		[]string{"missing", "lease"},
		[]string{"missing", "lease"},
	)
	if _, ok := registry.Context("lease"); !ok {
		t.Fatal("confirmed lease did not expose a context")
	}
}
