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
