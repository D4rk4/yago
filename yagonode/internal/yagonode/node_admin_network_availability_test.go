package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/seedimport"
)

func TestNetworkSourceKnowsSeedlistHasNoImportHistory(t *testing.T) {
	url := "https://seeds.example/seed.txt"
	source := newNetworkSource(
		dhtGateStatusSource{},
		nil,
		[]string{url},
		fakeSeedStatus{statuses: map[string]seedimport.Status{}},
		nil,
	)

	entries := source.Network(context.Background()).Seedlists
	if len(entries) != 1 || !entries[0].StatusKnown || entries[0].Imported {
		t.Fatalf("known empty import history = %+v", entries)
	}
}

func TestNetworkSourcesRejectFutureAdvertisedTimes(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	seed := networkTestSeed(t)
	seed.BirthDate = yagomodel.Some(yagomodel.NewSeedBirthDateUTC(now.Add(24 * time.Hour)))
	seed.LastSeen = yagomodel.Some(yagomodel.NewSeedLastSeenUTC(now.Add(time.Hour)))
	seed.Uptime = yagomodel.Some(600)

	network := newNetworkSource(
		dhtGateStatusSource{}, reachableRoster{peers: []yagomodel.Seed{seed}}, nil, nil, nil,
	)
	network.now = func() time.Time { return now }
	peers := network.Network(context.Background()).Peers
	if len(peers) != 1 || peers[0].AgeKnown || peers[0].HealthKnown ||
		peers[0].Health != 0 || peers[0].LastSeen != "" || !peers[0].LastSeenAt.IsZero() {
		t.Fatalf("future advertised network times = %+v, want unavailable", peers)
	}

	detailSource := newPeerDetailSource(reachableRoster{peers: []yagomodel.Seed{seed}}, nil)
	detailSource.now = func() time.Time { return now }
	detail, ok, err := detailSource.PeerDetail(context.Background(), string(seed.Hash))
	if err != nil || !ok || detail.AgeKnown || detail.LastSeen != "" {
		t.Fatalf("future advertised peer detail = %+v/%v/%v, want unavailable", detail, ok, err)
	}
}

func TestNetworkSourcesUseLocalRosterObservation(t *testing.T) {
	observedAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	roster, err := peerroster.Open(storage, func() time.Time { return observedAt }, 8, 4)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}
	seed := networkTestSeed(t)
	seed.LastSeen = yagomodel.Some(yagomodel.NewSeedLastSeenUTC(observedAt.Add(24 * time.Hour)))
	roster.Discover(t.Context(), seed)

	network := newNetworkSource(dhtGateStatusSource{}, roster, nil, nil, nil)
	network.now = func() time.Time { return observedAt.Add(time.Minute) }
	status := network.Network(t.Context())
	if !status.RosterAvailable || status.KnownPeers != 1 || status.ReachablePeers != 0 ||
		len(status.Peers) != 1 || status.Peers[0].LastSeen != observedAt.Format(time.RFC3339) {
		t.Fatalf("locally observed roster = %+v", status)
	}

	detailSource := newPeerDetailSource(roster, nil)
	detailSource.now = network.now
	detail, found, err := detailSource.PeerDetail(t.Context(), string(seed.Hash))
	if err != nil || !found || detail.LastSeen != observedAt.Format(time.RFC3339) {
		t.Fatalf("locally observed detail = %+v/%v/%v", detail, found, err)
	}
	if _, found, err := detailSource.PeerDetail(t.Context(), "GGGGGGGGGGGG"); err != nil || found {
		t.Fatalf("unknown observed peer = %v/%v", found, err)
	}
}

func TestNetworkSourceRendersLocallyObservedJuniorCaller(t *testing.T) {
	observedAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	roster, err := peerroster.Open(storage, func() time.Time { return observedAt }, 8, 4)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}
	caller := networkTestSeed(t)
	caller.Hash = yagomodel.Hash("JJJJJJJJJJJJ")
	caller.Name = yagomodel.Some("juniorCaller")
	roster.ObserveCaller(t.Context(), caller, yagomodel.PeerJunior)

	visibleRoster := newBlockingRoster(roster, newFakePeerBlocks())
	status := newNetworkSource(
		dhtGateStatusSource{}, visibleRoster, nil, nil, nil,
	).Network(t.Context())
	if !status.RosterAvailable || status.KnownPeers != 1 || status.ReachablePeers != 0 ||
		len(status.Peers) != 1 || status.Peers[0].Name != "juniorCaller" ||
		status.Peers[0].Type != "junior" || status.Peers[0].Address != "1.2.3.4:8090" ||
		status.Peers[0].LastSeen != observedAt.Format(time.RFC3339) {
		t.Fatalf("junior Admin projection = %+v", status)
	}
}

func TestNetworkSourceKeepsBlockedActivePeerVisibleButNotReachable(t *testing.T) {
	observedAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	roster, err := peerroster.Open(storage, func() time.Time { return observedAt }, 8, 4)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}
	peer := networkTestSeed(t)
	roster.ObserveCaller(t.Context(), peer, yagomodel.PeerSenior)
	blocks := newFakePeerBlocks(peer.Hash)
	visibleRoster := newBlockingRoster(roster, blocks)

	status := newNetworkSource(
		dhtGateStatusSource{}, visibleRoster, nil, nil, blocks,
	).Network(t.Context())
	if !status.RosterAvailable || status.KnownPeers != 1 || status.ReachablePeers != 0 ||
		len(status.Peers) != 1 || !status.Peers[0].Blocked ||
		!status.Peers[0].BlockStatusKnown || status.Peers[0].Hash != peer.Hash.String() {
		t.Fatalf("blocked active Admin projection = %+v", status)
	}
}

func TestNetworkSourcesKeepClosedRosterUnavailable(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	roster, err := peerroster.Open(storage, time.Now, 8, 4)
	if err != nil {
		t.Fatalf("peerroster.Open: %v", err)
	}
	seed := networkTestSeed(t)
	roster.Discover(t.Context(), seed)
	roster.ConfirmReachable(t.Context(), seed.Hash)
	if err := storage.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	status := newNetworkSource(
		dhtGateStatusSource{}, roster, nil, nil, nil,
	).Network(t.Context())
	if status.RosterAvailable || status.KnownPeers != 0 || status.ReachablePeers != 0 ||
		len(status.Peers) != 0 {
		t.Fatalf("closed roster = %+v, want unavailable without false counts", status)
	}
	if _, found, err := newPeerDetailSource(roster, nil).PeerDetail(
		t.Context(), string(seed.Hash),
	); err == nil || found {
		t.Fatalf("closed peer lookup = %v/%v, want unavailable", found, err)
	}
}

func TestSeedWithMissingLocalObservationClearsAdvertisedTime(t *testing.T) {
	seed := networkTestSeed(t)
	seed = seedWithLocalObservation(seed, time.Time{})
	if _, known := seed.LastSeen.Get(); known {
		t.Fatal("a missing local observation must not preserve advertised recency")
	}
}
