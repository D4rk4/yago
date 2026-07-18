package crawllease

import (
	"testing"
	"time"
)

func TestGrantRegistrySignalsRejectedAndExpiredLeaseLoss(t *testing.T) {
	now := time.Unix(30_000, 0)
	registry := newGrantRegistry(t.Context(), 1, func() time.Time { return now }, false)
	losses := registry.LeaseLosses()
	registry.Reject("missing")
	rejectLeaseLoss(t, losses)
	if err := registry.Track("rejected"); err != nil {
		t.Fatal(err)
	}
	registry.Renew(now, time.Minute, []string{"rejected"}, []string{"rejected"})
	losses = registry.LeaseLosses()
	registry.Reject("rejected")
	awaitLeaseLoss(t, losses)
	if err := registry.Track("expired"); err != nil {
		t.Fatal(err)
	}
	registry.Renew(now, time.Second, []string{"expired"}, []string{"expired"})
	losses = registry.LeaseLosses()
	now = now.Add(time.Second)
	if registry.Confirmed("expired") {
		t.Fatal("expired lease remained confirmed")
	}
	awaitLeaseLoss(t, losses)
}

func TestGrantRegistryDoesNotSignalIntentionalRevocationAsLeaseLoss(t *testing.T) {
	registry := NewGrantRegistry(t.Context(), 1)
	if err := registry.Track("settled"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	registry.Renew(started, time.Hour, []string{"settled"}, []string{"settled"})
	losses := registry.LeaseLosses()
	registry.Revoke("settled")
	rejectLeaseLoss(t, losses)
}

func awaitLeaseLoss(t *testing.T, losses <-chan struct{}) {
	t.Helper()
	select {
	case <-losses:
	case <-time.After(time.Second):
		t.Fatal("lease loss was not signaled")
	}
}

func rejectLeaseLoss(t *testing.T, losses <-chan struct{}) {
	t.Helper()
	select {
	case <-losses:
		t.Fatal("lease loss was signaled")
	case <-time.After(20 * time.Millisecond):
	}
}
