package services

import (
	"context"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func testIdentity() yacymodel.PeerIdentity {
	return yacymodel.PeerIdentity{
		Hash:        hashFor("self"),
		NetworkName: "freeworld",
		Name:        "node1",
		Host:        "10.0.0.9",
		Port:        8090,
		Flags:       yacymodel.ZeroFlags(),
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
	if snap.Seed.Hash != hashFor("self") {
		t.Errorf("seed hash: got %q", snap.Seed.Hash)
	}
	if rwi, _ := snap.Seed.RWICount.Get(); rwi != 11 {
		t.Errorf("seed rwi count: got %d, want 11", rwi)
	}
	if url, _ := snap.Seed.URLCount.Get(); url != 22 {
		t.Errorf("seed url count: got %d, want 22", url)
	}
	if pt, ok := snap.Seed.PeerType.Get(); !ok || pt != yacymodel.PeerSenior {
		t.Errorf("peer type: got %q", pt)
	}
}

func TestSnapshotToleratesCountErrors(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	rwi := &fakeRWIStore{rwiCountErr: context.Canceled}
	urls := &fakeURLStore{countErr: context.Canceled}
	status := NewRuntimeStatus(testIdentity(), clock, rwi, urls, "0.1.0")

	snap := status.Snapshot(context.Background())
	rwiCount, _ := snap.Seed.RWICount.Get()
	urlCount, _ := snap.Seed.URLCount.Get()
	if rwiCount != 0 || urlCount != 0 {
		t.Errorf(
			"expected zero counts on error, got rwi=%d url=%d",
			rwiCount,
			urlCount,
		)
	}
}
