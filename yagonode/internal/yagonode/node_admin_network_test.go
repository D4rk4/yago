package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
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
	source := newNetworkSource(dhtGateStatusSource{}, nil, nil, nil, nil)
	peers := source.adminNetworkPeers(context.Background(), []yagomodel.Seed{networkTestSeed(t)})
	if len(peers) != 1 {
		t.Fatalf("peers = %d", len(peers))
	}
	peer := peers[0]
	if peer.Name != "peerA" || peer.Type != "senior" || peer.RWICount != 42 {
		t.Fatalf("peer = %+v", peer)
	}
	if !peer.RWIKnown {
		t.Fatal("peer RWI count should be known")
	}
	if !peer.BlockStatusKnown {
		t.Fatal("an absent blocklist means the peer is effectively unblocked")
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

func TestNetworkSourceComputesHealthFromCompleteStatistics(t *testing.T) {
	now := time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)
	seed := networkTestSeed(t)
	seed.BirthDate = yagomodel.Some(
		yagomodel.NewSeedBirthDateUTC(now.Add(-48 * time.Hour)),
	)
	seed.Uptime = yagomodel.Some(600)
	source := newNetworkSource(dhtGateStatusSource{}, nil, nil, nil, nil)
	source.now = func() time.Time { return now }

	peers := source.adminNetworkPeers(t.Context(), []yagomodel.Seed{seed})
	if len(peers) != 1 || !peers[0].HealthKnown || !peers[0].AgeKnown ||
		peers[0].AgeDays != 2 {
		t.Fatalf("complete peer health = %+v", peers)
	}
}

func TestNetworkSourceSurfacesReachabilityAndSeedlists(t *testing.T) {
	gates := dhtGateStatusSource{
		snapshotWithReachability: func(context.Context) (
			dhtexchange.GateState,
			publicReachabilitySnapshot,
		) {
			return dhtexchange.GateState{}, publicReachabilitySnapshot{
				state: publicReachabilityReachable,
			}
		},
	}
	roster := reachableRoster{peers: []yagomodel.Seed{networkTestSeed(t)}}
	source := newNetworkSource(gates, roster, []string{"https://seeds.example/seed.txt"}, nil, nil)

	status := source.Network(context.Background())
	if !status.PublicReachable || !status.PublicReachabilityKnown {
		t.Fatal("expected the public self-test result to be surfaced")
	}
	if len(status.Seedlists) != 1 || status.Seedlists[0].URL != "https://seeds.example/seed.txt" {
		t.Fatalf("seedlists = %+v", status.Seedlists)
	}
	if status.Seedlists[0].Imported {
		t.Fatal("a seedlist with no recorded import should not be marked imported")
	}
	if len(status.Peers) != 1 || status.KnownPeers != 1 {
		t.Fatalf("peers = %+v known=%d", status.Peers, status.KnownPeers)
	}
}

func TestNetworkSourceSurfacesReachabilityProvenance(t *testing.T) {
	observedAt := time.Unix(12, 0)
	gates := dhtGateStatusSource{
		snapshotWithReachability: func(context.Context) (
			dhtexchange.GateState,
			publicReachabilitySnapshot,
		) {
			return dhtexchange.GateState{}, publicReachabilitySnapshot{
				state: publicReachabilityReachable, source: publicReachabilitySourcePeerBackPing,
				observedAt: observedAt,
			}
		},
	}
	status := newNetworkSource(gates, nil, nil, nil, nil).Network(t.Context())
	if !status.PublicReachabilityKnown || !status.PublicReachable ||
		status.PublicReachabilitySource != adminui.PublicReachabilityPeerBackPing ||
		status.PublicReachabilityObservedAt != "1970-01-01T00:00:12Z" {
		t.Fatalf("network reachability provenance = %+v", status)
	}
}

func TestAdminReachabilityObservedAtFormatsUTCAndZero(t *testing.T) {
	if got := adminReachabilityObservedAt(time.Time{}); got != "" {
		t.Fatalf("zero observation = %q", got)
	}
	observed := time.Date(2026, time.July, 21, 5, 4, 3, 0, time.FixedZone("test", 2*60*60))
	if got := adminReachabilityObservedAt(observed); got != "2026-07-21T03:04:03Z" {
		t.Fatalf("UTC observation = %q", got)
	}
}

func TestAdminReachabilitySourceMapsEveryProvenance(t *testing.T) {
	for source, want := range map[publicReachabilitySource]string{
		publicReachabilitySourcePeerBackPing: adminui.PublicReachabilityPeerBackPing,
		publicReachabilitySourcePinnedProbe:  adminui.PublicReachabilityPinnedProbe,
		publicReachabilitySourceDerivedProbe: adminui.PublicReachabilityDerivedProbe,
		publicReachabilitySourceUnspecified:  "",
	} {
		if got := adminReachabilitySource(source); got != want {
			t.Fatalf("source %q = %q, want %q", source, got, want)
		}
	}
}

type boundedNetworkRoster struct {
	reachableRoster
	requested int
}

func (r *boundedNetworkRoster) FreshestPeers(
	_ context.Context,
	limit int,
) []yagomodel.Seed {
	r.requested = limit

	return r.peers[:min(limit, len(r.peers))]
}

func TestNetworkSourceIncludesCompleteRoster(t *testing.T) {
	peers := make([]yagomodel.Seed, 270)
	for i := range peers {
		peers[i] = networkTestSeed(t)
	}
	roster := &boundedNetworkRoster{reachableRoster: reachableRoster{peers: peers}}
	source := newNetworkSource(dhtGateStatusSource{}, roster, nil, nil, nil)

	status := source.Network(context.Background())
	if roster.requested != 270 || status.KnownPeers != 270 || len(status.Peers) != 270 {
		t.Fatalf(
			"requested=%d known=%d peers=%d",
			roster.requested,
			status.KnownPeers,
			len(status.Peers),
		)
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
	source := newPeerDetailSource(reachableRoster{peers: []yagomodel.Seed{seed}}, nil)

	detail, ok, err := source.PeerDetail(context.Background(), string(seed.Hash))
	if err != nil || !ok {
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
	if !detail.RWIWordsKnown || !detail.URLsKnown || !detail.KnownSeedsKnown ||
		!detail.UptimeKnown || !detail.SentWordsKnown || !detail.ReceivedWordsKnown ||
		!detail.SentURLsKnown || !detail.ReceivedURLsKnown || !detail.BlockStatusKnown {
		t.Fatalf("known peer statistics lost availability: %+v", detail)
	}
}

func TestPeerDetailSourceKeepsMissingAndNegativeStatisticsUnavailable(t *testing.T) {
	seed := networkTestSeed(t)
	seed.BirthDate = yagomodel.None[yagomodel.SeedBirthDateUTC]()
	seed.Uptime = yagomodel.Some(-1)
	seed.RWICount = yagomodel.Some(-2)
	seed.URLCount = yagomodel.None[int]()
	seed.KnownSeedCount = yagomodel.None[int]()
	seed.SentWordCount = yagomodel.Some[int64](-3)
	source := newPeerDetailSource(reachableRoster{peers: []yagomodel.Seed{seed}}, nil)

	detail, ok, err := source.PeerDetail(context.Background(), string(seed.Hash))
	if err != nil || !ok {
		t.Fatal("a known peer must resolve")
	}
	if detail.AgeKnown || detail.UptimeKnown || detail.RWIWordsKnown || detail.URLsKnown ||
		detail.KnownSeedsKnown || detail.SentWordsKnown {
		t.Fatalf("invalid or missing peer facts marked known: %+v", detail)
	}
}

func TestPeerDetailSourceRejectsMalformedHash(t *testing.T) {
	source := newPeerDetailSource(reachableRoster{}, nil)
	if _, ok, err := source.PeerDetail(context.Background(), "too-short"); err != nil || ok {
		t.Fatal("a malformed hash must not resolve")
	}
}

func TestPeerDetailSourceReportsUnknownPeer(t *testing.T) {
	source := newPeerDetailSource(reachableRoster{}, nil)
	if _, ok, err := source.PeerDetail(
		context.Background(), "HHHHHHHHHHHH",
	); err != nil || ok {
		t.Fatal("an unknown but well-formed hash must not resolve")
	}
	if _, ok, err := newPeerDetailSource(nil, nil).PeerDetail(
		context.Background(), "HHHHHHHHHHHH",
	); err != nil || ok {
		t.Fatal("an unavailable roster must not resolve a peer")
	}
}
