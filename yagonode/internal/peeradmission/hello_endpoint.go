package peeradmission

import (
	"context"
	"crypto/rand"
	"log/slog"
	"math/big"
	"slices"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type callerReachabilityProbe interface {
	Reachable(
		ctx context.Context,
		caller yagomodel.Seed,
		self yagomodel.Hash,
		networkName string,
	) bool
}

type ReachableRoster interface {
	ReachablePeers(ctx context.Context) []yagomodel.Seed
	ConfirmReachable(ctx context.Context, peer yagomodel.Hash)
}

var randomPeerIndex = rand.Int

type helloEndpoint struct {
	identity     nodeidentity.Identity
	status       RuntimeStatus
	probe        callerReachabilityProbe
	reachability ReachableRoster
	news         NewsIntake
}

func (e helloEndpoint) Serve(
	ctx context.Context,
	req yagoproto.HelloRequest,
) (yagoproto.HelloResponse, error) {
	resp := yagoproto.HelloResponse{
		YourIP: httpguard.RemoteAddr(ctx),
		Seeds:  []yagomodel.Seed{e.status.SelfSeed(ctx)},
	}

	if e.identity.NetworkMatches(req.NetworkName) {
		resp.YourType = e.classifyCaller(ctx, req.Seed)
		resp.Seeds = append(resp.Seeds, e.knownPeers(ctx, req.Count)...)
		e.acceptCallerNews(ctx, req.Seed, resp.YourType)
	}

	slog.DebugContext(ctx, "hello served", slog.Int("seedCount", len(resp.Seeds)))

	return resp, nil
}

func (e helloEndpoint) acceptCallerNews(
	ctx context.Context,
	caller yagomodel.Seed,
	callerType yagomodel.PeerType,
) {
	if e.news == nil || callerType == yagomodel.PeerVirgin {
		return
	}
	if attachment := caller.Properties()[yagomodel.SeedNews]; attachment != "" {
		e.news.AcceptNewsAttachment(ctx, attachment)
	}
}

func (e helloEndpoint) classifyCaller(
	ctx context.Context,
	caller yagomodel.Seed,
) yagomodel.PeerType {
	if samePeerIdentity(caller, e.status.SelfSeed(ctx)) {
		return yagomodel.PeerVirgin
	}

	if _, ok := caller.NetworkAddress(); !ok {
		return yagomodel.PeerJunior
	}

	if !e.probe.Reachable(ctx, caller, e.status.SelfSeed(ctx).Hash, e.status.NetworkName(ctx)) {
		return yagomodel.PeerJunior
	}

	e.reachability.ConfirmReachable(ctx, caller.Hash)

	return yagomodel.PeerSenior
}

func samePeerIdentity(caller, self yagomodel.Seed) bool {
	if caller.Hash == self.Hash {
		return true
	}

	callerPort, callerPortOK := caller.Port.Get()
	selfPort, selfPortOK := self.Port.Get()
	if !callerPortOK || !selfPortOK || callerPort != selfPort {
		return false
	}

	return caller.SharesAddress(self)
}

func (e helloEndpoint) knownPeers(ctx context.Context, count int) []yagomodel.Seed {
	known := slices.Clone(e.reachability.ReachablePeers(ctx))

	shuffleKnownPeers(known)

	if count > 0 && count < len(known) {
		known = known[:count]
	}

	return known
}

func shuffleKnownPeers(peers []yagomodel.Seed) {
	for last := len(peers) - 1; last > 0; last-- {
		selected, err := randomPeerIndex(rand.Reader, big.NewInt(int64(last+1)))
		if err != nil {
			return
		}
		current := int(selected.Int64())
		peers[last], peers[current] = peers[current], peers[last]
	}
}
