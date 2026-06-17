package services

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func testIdentity() Identity {
	return NewIdentity(
		hashFor("self"),
		"freeworld",
		"node1",
		"10.0.0.9",
		8090,
		yacymodel.ZeroFlags(),
	)
}

func TestIdentityAccessors(t *testing.T) {
	id := testIdentity()
	if id.Hash() != hashFor("self") {
		t.Errorf("hash: got %q", id.Hash())
	}
	if id.NetworkName() != "freeworld" {
		t.Errorf("network: got %q", id.NetworkName())
	}
}

func TestSnapshotComposesSeed(t *testing.T) {
	start := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	rwi := &fakeRWIStore{rwiCount: 11}
	urls := &fakeURLStore{urlCount: 22}
	status := NewRuntimeStatus(testIdentity(), clock, rwi, urls, "0.1.0")

	clock.now = start.Add(3 * time.Minute)
	snap := status.Snapshot(context.Background())

	if snap.Version != "0.1.0" {
		t.Errorf("version: got %q", snap.Version)
	}
	if snap.Uptime != 3 {
		t.Errorf("uptime: got %d, want 3", snap.Uptime)
	}
	if snap.Seed[yacymodel.SeedHash] != string(hashFor("self")) {
		t.Errorf("seed hash: got %q", snap.Seed[yacymodel.SeedHash])
	}
	if snap.Seed[seedRWICount] != "11" || snap.Seed[seedURLCount] != "22" {
		t.Errorf("seed counts: got rwi=%q url=%q", snap.Seed[seedRWICount], snap.Seed[seedURLCount])
	}
	if snap.Seed[yacymodel.SeedPeerType] != string(yacymodel.PeerSenior) {
		t.Errorf("peer type: got %q", snap.Seed[yacymodel.SeedPeerType])
	}
}

func TestSnapshotToleratesCountErrors(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	rwi := &fakeRWIStore{rwiCountErr: context.Canceled}
	urls := &fakeURLStore{countErr: context.Canceled}
	status := NewRuntimeStatus(testIdentity(), clock, rwi, urls, "0.1.0")

	snap := status.Snapshot(context.Background())
	if snap.Seed[seedRWICount] != "0" || snap.Seed[seedURLCount] != "0" {
		t.Errorf(
			"expected zero counts on error, got rwi=%q url=%q",
			snap.Seed[seedRWICount],
			snap.Seed[seedURLCount],
		)
	}
}
