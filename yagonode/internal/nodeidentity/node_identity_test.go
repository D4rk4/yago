package nodeidentity

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestIdentityUptimeRoundsDownToMinutes(t *testing.T) {
	start := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	identity := Identity{Start: start}

	if got := identity.Uptime(start.Add(90*time.Second + 59*time.Millisecond)); got != 1 {
		t.Fatalf("Uptime = %d, want 1", got)
	}
}

func TestIdentityNetworkMatchesDefaultNetwork(t *testing.T) {
	identity := Identity{NetworkName: "freeworld"}

	if !identity.NetworkMatches("") {
		t.Fatal("empty network should match default freeworld network")
	}
	if identity.NetworkMatches("other") {
		t.Fatal("different network should not match")
	}
}

func TestIdentityAddressesNetworkAndHash(t *testing.T) {
	hash := yagomodel.WordHash("self")
	identity := Identity{Hash: hash, NetworkName: "freeworld"}

	if !identity.Addresses("freeworld", hash) {
		t.Fatal("identity should address matching network and hash")
	}
	if identity.Addresses("freeworld", yagomodel.WordHash("other")) {
		t.Fatal("identity should reject a different hash")
	}
	if identity.Addresses("other", hash) {
		t.Fatal("identity should reject a different network")
	}
}
