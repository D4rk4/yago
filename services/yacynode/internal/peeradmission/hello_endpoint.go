package peeradmission

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"slices"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type callerReachabilityProbe interface {
	Reachable(
		ctx context.Context,
		caller yacymodel.Seed,
		self yacymodel.Hash,
		networkName string,
	) bool
}

type reachableRoster interface {
	ReachablePeers(ctx context.Context) []yacymodel.Seed
	ConfirmReachable(ctx context.Context, peer yacymodel.Hash)
}

type helloEndpoint struct {
	identity     nodeidentity.Identity
	status       RuntimeStatus
	probe        callerReachabilityProbe
	reachability reachableRoster
}

func (e helloEndpoint) Serve(
	ctx context.Context,
	req yacyproto.HelloRequest,
) (yacyproto.HelloResponse, error) {
	resp := yacyproto.HelloResponse{
		YourIP: httpguard.RemoteAddr(ctx),
		Seeds:  []yacymodel.Seed{e.status.SelfSeed(ctx)},
	}

	if e.identity.NetworkMatches(req.NetworkName) {
		resp.YourType = e.classifyCaller(ctx, req.Seed)
		resp.Seeds = append(resp.Seeds, e.knownPeers(ctx, req.Count)...)
	}

	slog.DebugContext(ctx, "hello served", slog.Int("seedCount", len(resp.Seeds)))

	return resp, nil
}

func (e helloEndpoint) classifyCaller(
	ctx context.Context,
	caller yacymodel.Seed,
) yacymodel.PeerType {
	if _, ok := caller.NetworkAddress(); !ok {
		return yacymodel.PeerJunior
	}

	if !e.probe.Reachable(ctx, caller, e.status.SelfSeed(ctx).Hash, e.status.NetworkName(ctx)) {
		return yacymodel.PeerJunior
	}

	e.reachability.ConfirmReachable(ctx, caller.Hash)

	return yacymodel.PeerSenior
}

func (e helloEndpoint) knownPeers(ctx context.Context, count int) []yacymodel.Seed {
	known := slices.Clone(e.reachability.ReachablePeers(ctx))

	rand.Shuffle(len(known), func(i, j int) {
		known[i], known[j] = known[j], known[i]
	})

	if count > 0 && count < len(known) {
		known = known[:count]
	}

	return known
}
