package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

// TestSearchTargetPeersOffersKnownUnconfirmedPeers proves the YaCy-aligned fix:
// remote-search targets are drawn from known senior peers, not only peers a
// prior hello confirmed reachable. A node behind NAT never confirms its own
// inbound reachability, yet must still search the network, so a discovered-but-
// unconfirmed senior peer must remain a valid search target.
func TestSearchTargetPeersOffersKnownUnconfirmedPeers(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	roster, err := peerroster.Open(
		t.Context(),
		openTestVault(t),
		yagomodel.Hash("LLLLLLLLLLLL"),
		func() time.Time { return now },
		peerroster.Capacity{Reservoir: reservoirCapacity, Active: activeSetCapacity},
	)
	if err != nil {
		t.Fatalf("open roster: %v", err)
	}
	ctx := context.Background()

	seed := networkTestSeed(t)
	seed.LastSeen = yagomodel.Some(yagomodel.NewSeedLastSeenUTC(now))
	roster.Discover(ctx, seed) // known, but never ConfirmReachable
	junior := seed
	junior.Hash = yagomodel.Hash("JJJJJJJJJJJJ")
	junior.Name = yagomodel.Some("junior")
	roster.ObserveCaller(ctx, junior, yagomodel.PeerJunior)

	if n := roster.ReachablePeerCount(ctx); n != 0 {
		t.Fatalf("reachable peers = %d, want 0 (never confirmed)", n)
	}

	targets := searchTargetPeers{roster: roster}.SearchTargetPeers(ctx)
	if len(targets) != 1 || targets[0].Hash != seed.Hash {
		t.Fatalf("search targets = %v, want only known senior %q", targets, seed.Hash)
	}
}
