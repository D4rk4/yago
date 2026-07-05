package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

// TestSearchTargetPeersOffersKnownUnconfirmedPeers proves the YaCy-aligned fix:
// remote-search targets are drawn from known senior peers, not only peers a
// prior hello confirmed reachable. A node behind NAT never confirms its own
// inbound reachability, yet must still search the network, so a discovered-but-
// unconfirmed senior peer must remain a valid search target.
func TestSearchTargetPeersOffersKnownUnconfirmedPeers(t *testing.T) {
	roster, err := peerroster.Open(
		openTestVault(t), time.Now, reservoirCapacity, activeSetCapacity,
	)
	if err != nil {
		t.Fatalf("open roster: %v", err)
	}
	ctx := context.Background()

	seed := networkTestSeed(t)
	roster.Discover(ctx, seed) // known, but never ConfirmReachable

	if n := roster.ReachablePeerCount(ctx); n != 0 {
		t.Fatalf("reachable peers = %d, want 0 (never confirmed)", n)
	}

	targets := searchTargetPeers{roster: roster}.SearchTargetPeers(ctx)
	if len(targets) != 1 || targets[0].Hash != seed.Hash {
		t.Fatalf("search targets = %v, want the known peer %q", targets, seed.Hash)
	}
}
