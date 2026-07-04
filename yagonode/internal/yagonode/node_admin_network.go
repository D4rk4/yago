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
	gates  dhtGateStatusSource
	roster peerroster.Roster
	now    func() time.Time
}

func newNetworkSource(gates dhtGateStatusSource, roster peerroster.Roster) networkSource {
	return networkSource{gates: gates, roster: roster, now: time.Now}
}

func (s networkSource) Network(ctx context.Context) adminui.NetworkStatus {
	report := s.gates.response(ctx)
	status := adminui.NetworkStatus{
		Available:      true,
		DHTOpen:        report.Open,
		BlockingReason: report.BlockingReason,
		Gates:          adminNetworkGates(report.Gates),
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
		peers = append(peers, adminui.NetworkPeer{
			Name:    name,
			Hash:    string(seed.Hash),
			Address: address,
			AgeDays: seed.AgeDays(now),
		})
	}

	return peers
}
