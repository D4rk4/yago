package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhtexchange"
)

func networkTestSeed(t *testing.T) yagomodel.Seed {
	t.Helper()

	host, err := yagomodel.ParseHost("1.2.3.4")
	if err != nil {
		t.Fatalf("parse host: %v", err)
	}
	lastSeen := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	return yagomodel.Seed{
		Hash:     yagomodel.Hash("HHHHHHHHHHHH"),
		Name:     yagomodel.Some("peerA"),
		IP:       yagomodel.Some(host),
		Port:     yagomodel.Some(yagomodel.Port(8090)),
		PeerType: yagomodel.Some(yagomodel.PeerSenior),
		RWICount: yagomodel.Some(42),
		Flags: yagomodel.Some(
			yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true),
		),
		LastSeen: yagomodel.Some(yagomodel.NewSeedLastSeenUTC(lastSeen)),
	}
}

func TestNetworkSourceMapsPeerDetail(t *testing.T) {
	source := newNetworkSource(dhtGateStatusSource{}, nil, nil)
	peers := source.adminNetworkPeers([]yagomodel.Seed{networkTestSeed(t)})
	if len(peers) != 1 {
		t.Fatalf("peers = %d", len(peers))
	}
	peer := peers[0]
	if peer.Name != "peerA" || peer.Type != "senior" || peer.RWICount != 42 {
		t.Fatalf("peer = %+v", peer)
	}
	if peer.Address != "1.2.3.4:8090" {
		t.Fatalf("address = %q", peer.Address)
	}
	if len(peer.Flags) != 1 || peer.Flags[0] != "remote-index" {
		t.Fatalf("flags = %v", peer.Flags)
	}
	if peer.LastSeen != "2026-01-02T03:04:05Z" {
		t.Fatalf("lastSeen = %q", peer.LastSeen)
	}
}

func TestNetworkSourceSurfacesReachabilityAndSeedlists(t *testing.T) {
	gates := dhtGateStatusSource{
		snapshot: func(context.Context) dhtexchange.GateState {
			return dhtexchange.GateState{PublicReachable: true}
		},
	}
	roster := reachableRoster{peers: []yagomodel.Seed{networkTestSeed(t)}}
	source := newNetworkSource(gates, roster, []string{"https://seeds.example/seed.txt"})

	status := source.Network(context.Background())
	if !status.PublicReachable {
		t.Fatal("expected the public self-test result to be surfaced")
	}
	if len(status.SeedlistURLs) != 1 || status.SeedlistURLs[0] != "https://seeds.example/seed.txt" {
		t.Fatalf("seedlist urls = %v", status.SeedlistURLs)
	}
	if len(status.Peers) != 1 || status.KnownPeers != 1 {
		t.Fatalf("peers = %+v known=%d", status.Peers, status.KnownPeers)
	}
}

func TestSeedFlagLabelsEmptyWithoutFlags(t *testing.T) {
	if labels := seedFlagLabels(yagomodel.Seed{}); labels != nil {
		t.Fatalf("expected no labels for a seed without flags, got %v", labels)
	}
}

func TestPeerDetailSourceMapsSeed(t *testing.T) {
	seed := networkTestSeed(t)
	seed.Version = yagomodel.Some(yagomodel.YaCyVersion("1.83"))
	seed.URLCount = yagomodel.Some(1234)
	seed.KnownSeedCount = yagomodel.Some(9)
	seed.Uptime = yagomodel.Some(600)
	seed.SentWordCount = yagomodel.Some(int64(11))
	seed.ReceivedWordCount = yagomodel.Some(int64(22))
	seed.SentURLCount = yagomodel.Some(int64(33))
	seed.ReceivedURLCount = yagomodel.Some(int64(44))
	source := newPeerDetailSource(reachableRoster{peers: []yagomodel.Seed{seed}})

	detail, ok := source.PeerDetail(context.Background(), string(seed.Hash))
	if !ok {
		t.Fatal("a known peer must resolve")
	}
	switch {
	case detail.Name != "peerA" || detail.Type != "senior" || detail.Hash != "HHHHHHHHHHHH":
		t.Fatalf("identity = %+v", detail)
	case detail.Address != "1.2.3.4:8090" || detail.Version != "1.83":
		t.Fatalf("address/version = %+v", detail)
	case detail.RWIWords != 42 || detail.URLs != 1234 || detail.KnownSeeds != 9 || detail.UptimeMinutes != 600:
		t.Fatalf("stats = %+v", detail)
	case detail.SentWords != 11 || detail.ReceivedWords != 22 || detail.SentURLs != 33 || detail.ReceivedURLs != 44:
		t.Fatalf("transfer counters = %+v", detail)
	case len(detail.Flags) != 1 || detail.Flags[0] != "remote-index":
		t.Fatalf("flags = %v", detail.Flags)
	}
}

func TestPeerDetailSourceRejectsMalformedHash(t *testing.T) {
	source := newPeerDetailSource(reachableRoster{})
	if _, ok := source.PeerDetail(context.Background(), "too-short"); ok {
		t.Fatal("a malformed hash must not resolve")
	}
}

func TestPeerDetailSourceReportsUnknownPeer(t *testing.T) {
	source := newPeerDetailSource(reachableRoster{})
	if _, ok := source.PeerDetail(context.Background(), "HHHHHHHHHHHH"); ok {
		t.Fatal("an unknown but well-formed hash must not resolve")
	}
}
