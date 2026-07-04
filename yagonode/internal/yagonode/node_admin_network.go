package yagonode

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

const adminNetworkPeerLimit = 20

type networkSource struct {
	gates        dhtGateStatusSource
	roster       peerroster.Roster
	seedlistURLs []string
	now          func() time.Time
}

func newNetworkSource(
	gates dhtGateStatusSource,
	roster peerroster.Roster,
	seedlistURLs []string,
) networkSource {
	return networkSource{gates: gates, roster: roster, seedlistURLs: seedlistURLs, now: time.Now}
}

func (s networkSource) Network(ctx context.Context) adminui.NetworkStatus {
	report := s.gates.response(ctx)
	status := adminui.NetworkStatus{
		Available:       true,
		DHTOpen:         report.Open,
		PublicReachable: report.State.PublicReachable,
		BlockingReason:  report.BlockingReason,
		Gates:           adminNetworkGates(report.Gates),
		SeedlistURLs:    s.seedlistURLs,
	}

	if s.roster != nil {
		status.KnownPeers = s.roster.KnownPeerCount(ctx)
		status.ReachablePeers = s.roster.ReachablePeerCount(ctx)
		status.Peers = s.adminNetworkPeers(s.roster.FreshestPeers(ctx, adminNetworkPeerLimit))
	}

	return status
}

func adminNetworkGates(results []dhtGateResultResponse) []adminui.NetworkGate {
	gates := make([]adminui.NetworkGate, 0, len(results))
	for _, result := range results {
		gates = append(gates, adminui.NetworkGate{
			Name:   result.Name,
			Open:   result.Open,
			Reason: result.Reason,
		})
	}

	return gates
}

func (s networkSource) adminNetworkPeers(seeds []yagomodel.Seed) []adminui.NetworkPeer {
	now := s.now()
	peers := make([]adminui.NetworkPeer, 0, len(seeds))
	for _, seed := range seeds {
		name, _ := seed.Name.Get()
		address, _ := seed.NetworkAddress()
		peerType, _ := seed.PeerType.Get()
		rwiCount, _ := seed.RWICount.Get()
		peers = append(peers, adminui.NetworkPeer{
			Name:     name,
			Hash:     string(seed.Hash),
			Address:  address,
			Type:     string(peerType),
			Flags:    seedFlagLabels(seed),
			RWICount: rwiCount,
			LastSeen: seedLastSeen(seed),
			AgeDays:  seed.AgeDays(now),
		})
	}

	return peers
}

// peerDetailSource resolves a peer hash to its published profile for the console's
// per-peer detail page. It reads through the roster and carries no secrets.
type peerDetailSource struct {
	roster peerroster.Roster
	now    func() time.Time
}

func newPeerDetailSource(roster peerroster.Roster) peerDetailSource {
	return peerDetailSource{roster: roster, now: time.Now}
}

func (s peerDetailSource) PeerDetail(
	ctx context.Context,
	hash string,
) (adminui.PeerDetail, bool) {
	parsed, err := yagomodel.ParseHash(hash)
	if err != nil {
		return adminui.PeerDetail{}, false
	}
	seed, ok := s.roster.PeerByHash(ctx, parsed)
	if !ok {
		return adminui.PeerDetail{}, false
	}

	return peerDetailFromSeed(seed, s.now()), true
}

func peerDetailFromSeed(seed yagomodel.Seed, now time.Time) adminui.PeerDetail {
	name, _ := seed.Name.Get()
	address, _ := seed.NetworkAddress()
	peerType, _ := seed.PeerType.Get()
	rwi, _ := seed.RWICount.Get()
	urls, _ := seed.URLCount.Get()
	knownSeeds, _ := seed.KnownSeedCount.Get()
	uptime, _ := seed.Uptime.Get()
	sentWords, _ := seed.SentWordCount.Get()
	receivedWords, _ := seed.ReceivedWordCount.Get()
	sentURLs, _ := seed.SentURLCount.Get()
	receivedURLs, _ := seed.ReceivedURLCount.Get()

	version := ""
	if v, ok := seed.Version.Get(); ok {
		version = v.String()
	}

	return adminui.PeerDetail{
		Name:          name,
		Hash:          string(seed.Hash),
		Address:       address,
		Version:       version,
		Type:          string(peerType),
		Flags:         seedFlagLabels(seed),
		LastSeen:      seedLastSeen(seed),
		AgeDays:       seed.AgeDays(now),
		UptimeMinutes: uptime,
		RWIWords:      rwi,
		URLs:          urls,
		KnownSeeds:    knownSeeds,
		SentWords:     sentWords,
		ReceivedWords: receivedWords,
		SentURLs:      sentURLs,
		ReceivedURLs:  receivedURLs,
	}
}

var seedFlagBits = []struct {
	bit   int
	label string
}{
	{yagomodel.FlagDirectConnect, "direct"},
	{yagomodel.FlagAcceptRemoteCrawl, "remote-crawl"},
	{yagomodel.FlagAcceptRemoteIndex, "remote-index"},
	{yagomodel.FlagRootNode, "root"},
	{yagomodel.FlagSSLAvailable, "ssl"},
}

func seedFlagLabels(seed yagomodel.Seed) []string {
	flags, ok := seed.Flags.Get()
	if !ok {
		return nil
	}
	labels := make([]string, 0, len(seedFlagBits))
	for _, entry := range seedFlagBits {
		if flags.Get(entry.bit) {
			labels = append(labels, entry.label)
		}
	}

	return labels
}

func seedLastSeen(seed yagomodel.Seed) string {
	last, ok := seed.LastSeen.Get()
	if !ok {
		return ""
	}

	return last.Time().UTC().Format(time.RFC3339)
}
