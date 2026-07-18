package crawllease

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSettledGrantLeavesLeaseLossSignalQuiet(t *testing.T) {
	registry := NewGrantRegistry(t.Context(), 1)
	if err := registry.Track("settled"); err != nil {
		t.Fatalf("track settled grant: %v", err)
	}
	started := time.Now()
	registry.Renew(started, time.Hour, []string{"settled"}, []string{"settled"})
	grantContext, found := registry.Context("settled")
	if !found {
		t.Fatal("settled grant context is missing")
	}
	losses := registry.LeaseLosses()
	availability := registry.AvailabilityChanges()
	registry.Settle("settled")
	registry.Settle("settled")
	if active := registry.ActiveLeaseIDs(); len(active) != 0 {
		t.Fatalf("settled grant remained active: %v", active)
	}
	select {
	case <-grantContext.Done():
	case <-time.After(time.Second):
		t.Fatal("settled grant context remained active")
	}
	if cause := context.Cause(grantContext); !errors.Is(cause, context.Canceled) ||
		errors.Is(cause, ErrLeaseLost) {
		t.Fatalf("settled grant context cause = %v", cause)
	}
	select {
	case <-availability:
	default:
		t.Fatal("settled grant did not signal capacity availability")
	}
	select {
	case <-losses:
		t.Fatal("settled grant emitted lease loss")
	default:
	}
}

func TestSettlementProtectionDefersOmissionLossUntilFinalFailure(t *testing.T) {
	now := time.Unix(30_000, 0)
	registry := newGrantRegistry(t.Context(), 1, func() time.Time { return now }, false)
	confirmGrantForSettlement(t, registry, now, time.Hour)
	grantContext, found := registry.Context("settled")
	if !found {
		t.Fatal("protected grant context is missing")
	}
	losses := registry.LeaseLosses()
	if !registry.BeginSettlement("settled") {
		t.Fatal("first settlement protection was not established")
	}
	if !registry.BeginSettlement("settled") {
		t.Fatal("nested settlement protection was not established")
	}
	registry.Renew(
		now.Add(time.Second),
		time.Hour,
		[]string{"settled"},
		nil,
	)
	if !registry.Confirmed("settled") {
		t.Fatal("omitted protected grant was lost")
	}
	registry.SettlementFailed("settled")
	if !registry.Confirmed("settled") {
		t.Fatal("one failed nested attempt removed another attempt's protection")
	}
	select {
	case <-losses:
		t.Fatal("protected omission emitted lease loss")
	default:
	}
	registry.SettlementFailed("settled")
	if registry.Confirmed("settled") {
		t.Fatal("final failed attempt retained an omitted grant")
	}
	if !errors.Is(context.Cause(grantContext), ErrLeaseLost) {
		t.Fatalf("omitted grant context cause = %v", context.Cause(grantContext))
	}
	select {
	case <-losses:
	default:
		t.Fatal("restored omission semantics did not signal lease loss")
	}
	registry.SettlementFailed("settled")
}

func TestSettlementProtectionSuspendsLocalExpiry(t *testing.T) {
	now := time.Unix(40_000, 0)
	registry := newGrantRegistry(t.Context(), 1, func() time.Time { return now }, false)
	confirmGrantForSettlement(t, registry, now, time.Second)
	grantContext, found := registry.Context("settled")
	if !found {
		t.Fatal("expiring grant context is missing")
	}
	losses := registry.LeaseLosses()
	if !registry.BeginSettlement("settled") {
		t.Fatal("expiry settlement protection was not established")
	}
	now = now.Add(2 * time.Second)
	if active := registry.ActiveLeaseIDs(); len(active) != 1 || active[0] != "settled" {
		t.Fatalf("protected expired grants = %v", active)
	}
	if wait := registry.nextExpiry(); wait >= 0 {
		t.Fatalf("protected grant scheduled expiry wait %v", wait)
	}
	select {
	case <-losses:
		t.Fatal("protected expiry emitted lease loss")
	default:
	}
	registry.SettlementFailed("settled")
	if !errors.Is(context.Cause(grantContext), ErrLeaseLost) {
		t.Fatalf("expired grant context cause = %v", context.Cause(grantContext))
	}
}

func TestSettlementProtectionCannotResurrectExpiredGrant(t *testing.T) {
	now := time.Unix(45_000, 0)
	registry := newGrantRegistry(t.Context(), 1, func() time.Time { return now }, false)
	confirmGrantForSettlement(t, registry, now, time.Second)
	grantContext, found := registry.Context("settled")
	if !found {
		t.Fatal("expiring grant context is missing")
	}
	now = now.Add(2 * time.Second)
	if registry.BeginSettlement("settled") {
		t.Fatal("expired grant received settlement protection")
	}
	if !errors.Is(context.Cause(grantContext), ErrLeaseLost) {
		t.Fatalf("expired grant context cause = %v", context.Cause(grantContext))
	}
}

func TestLiveRenewalSupersedesProtectedOmission(t *testing.T) {
	now := time.Unix(50_000, 0)
	registry := newGrantRegistry(t.Context(), 1, func() time.Time { return now }, false)
	confirmGrantForSettlement(t, registry, now, time.Minute)
	if registry.BeginSettlement("missing") {
		t.Fatal("missing grant received settlement protection")
	}
	registry.SettlementFailed("missing")
	if !registry.BeginSettlement("settled") {
		t.Fatal("settlement protection was not established")
	}
	registry.Renew(
		now.Add(time.Second),
		time.Hour,
		[]string{"settled"},
		nil,
	)
	registry.Renew(
		now.Add(500*time.Millisecond),
		time.Hour,
		[]string{"settled"},
		nil,
	)
	registry.Renew(
		now.Add(2*time.Second),
		time.Hour,
		[]string{"settled"},
		[]string{"settled"},
	)
	registry.Renew(
		now.Add(1500*time.Millisecond),
		time.Hour,
		[]string{"settled"},
		nil,
	)
	registry.SettlementFailed("settled")
	if !registry.Confirmed("settled") {
		t.Fatal("newer live renewal did not supersede protected omission")
	}
	registry.Settle("settled")
	registry.Settle("settled")
}

func confirmGrantForSettlement(
	t *testing.T,
	registry *GrantRegistry,
	started time.Time,
	lifetime time.Duration,
) {
	t.Helper()
	if err := registry.Track("settled"); err != nil {
		t.Fatalf("track settlement grant: %v", err)
	}
	registry.Renew(started, lifetime, []string{"settled"}, []string{"settled"})
}
