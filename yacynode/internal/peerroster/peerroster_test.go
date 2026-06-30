package peerroster_test

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/memvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/peerroster"
)

const hashFiller = "AAAAAAAAAAAA"

func hashFor(base string) yacymodel.Hash {
	if len(base) >= yacymodel.HashLength {
		return yacymodel.Hash(base[:yacymodel.HashLength])
	}

	return yacymodel.Hash(base + hashFiller[len(base):])
}

func seniorSeed(t testing.TB, hash, ip string, port int) yacymodel.Seed {
	t.Helper()

	seed := yacymodel.Seed{Hash: hashFor(hash)}
	if ip != "" {
		host, err := yacymodel.ParseHost(ip)
		if err != nil {
			t.Fatalf("parse host: %v", err)
		}
		seed.IP = yacymodel.Some(host)
	}
	if port != 0 {
		seed.Port = yacymodel.Some(yacymodel.Port(port))
	}

	return seed
}

type tickingClock struct {
	now time.Time
}

func (c *tickingClock) Now() time.Time {
	c.now = c.now.Add(time.Second)

	return c.now
}

func openRoster(t *testing.T, reservoirCap, activeCap int) peerroster.Roster {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	clock := &tickingClock{now: time.Unix(1_000, 0)}
	roster, err := peerroster.Open(v, clock.Now, reservoirCap, activeCap)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}

	return roster
}

func hashes(seeds []yacymodel.Seed) map[yacymodel.Hash]struct{} {
	out := make(map[yacymodel.Hash]struct{}, len(seeds))
	for _, seed := range seeds {
		out[seed.Hash] = struct{}{}
	}

	return out
}

func TestDiscoverKeepsSeniorsAndDropsJuniors(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	senior := seniorSeed(t, "senior", "203.0.113.1", 8090)
	junior := seniorSeed(t, "junior", "", 0)
	roster.Discover(ctx, senior, junior)

	targets := hashes(roster.FreshestPeers(ctx, 4))
	if _, ok := targets[senior.Hash]; !ok {
		t.Fatalf("senior missing from greet targets: %v", targets)
	}
	if _, ok := targets[junior.Hash]; ok {
		t.Fatalf("junior should have been dropped: %v", targets)
	}
}

func TestReachablePromotesAndIsServed(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	senior := seniorSeed(t, "senior", "203.0.113.1", 8090)
	roster.Discover(ctx, senior)

	if got := roster.ReachablePeers(ctx); len(got) != 0 {
		t.Fatalf("reachable before greet = %d, want 0", len(got))
	}

	roster.ConfirmReachable(ctx, senior.Hash)

	if _, ok := hashes(roster.ReachablePeers(ctx))[senior.Hash]; !ok {
		t.Fatalf("senior not served as reachable after confirmation")
	}
}

func TestReachableUnknownPeerIsNoop(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	roster.ConfirmReachable(ctx, hashFor("ghost"))

	if got := roster.ReachablePeers(ctx); len(got) != 0 {
		t.Fatalf("reachable = %d, want 0 for unknown peer", len(got))
	}
}

func TestUnreachableDropsFromReachableAndReservoir(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 4)

	senior := seniorSeed(t, "senior", "203.0.113.1", 8090)
	roster.Discover(ctx, senior)
	roster.ConfirmReachable(ctx, senior.Hash)

	roster.ConfirmUnreachable(ctx, senior.Hash)

	if got := roster.ReachablePeers(ctx); len(got) != 0 {
		t.Fatalf("reachable = %d, want 0 after failure", len(got))
	}
	if got := roster.FreshestPeers(ctx, 4); len(got) != 0 {
		t.Fatalf("greet targets = %d, want 0 after drop", len(got))
	}
}

func TestDiscoverEvictsStalestBeyondCapacity(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 2, 4)

	oldest := seniorSeed(t, "oldest", "203.0.113.1", 8090)
	middle := seniorSeed(t, "middle", "203.0.113.2", 8090)
	newest := seniorSeed(t, "newest", "203.0.113.3", 8090)

	roster.Discover(ctx, oldest)
	roster.Discover(ctx, middle)
	roster.Discover(ctx, newest)

	targets := hashes(roster.FreshestPeers(ctx, 4))
	if _, ok := targets[oldest.Hash]; ok {
		t.Fatalf("stalest peer should have been evicted: %v", targets)
	}
	if len(targets) != 2 {
		t.Fatalf("reservoir size = %d, want 2 after eviction", len(targets))
	}
}

func TestFreshestPeersToppedUpToLimit(t *testing.T) {
	ctx := context.Background()
	roster := openRoster(t, 8, 2)

	for _, name := range []string{"a", "b", "c", "d"} {
		roster.Discover(ctx, seniorSeed(t, name, "203.0.113.9", 8090))
	}

	if got := len(roster.FreshestPeers(ctx, 2)); got != 2 {
		t.Fatalf("freshest peers = %d, want capped at limit 2", got)
	}
}
