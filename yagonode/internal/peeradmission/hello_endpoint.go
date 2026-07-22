package peeradmission

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type callerReachabilityProbe interface {
	ReachableCaller(
		ctx context.Context,
		caller yagomodel.Seed,
		self yagomodel.Hash,
		networkName string,
	) (yagomodel.Seed, bool)
}

type ReachableRoster interface {
	FreshestPeers(ctx context.Context, limit int) []yagomodel.Seed
	ObserveCaller(ctx context.Context, caller yagomodel.Seed, classification yagomodel.PeerType)
}

const (
	callerObservationTimeout = time.Second
	maximumHelloKnownPeers   = 100
)

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
		YourIP:   httpguard.RemoteAddr(ctx),
		YourType: yagomodel.PeerVirgin,
	}
	defer func() {
		slog.DebugContext(ctx, "hello served", slog.Int("seedCount", len(resp.Seeds)))
	}()

	if !e.identity.Authenticates(
		req.NetworkName,
		req.NetworkNamePresent,
		req.Key,
		req.Iam,
		req.MagicMD5,
	) {
		return resp, nil
	}

	caller, observable := observableHelloCaller(ctx, req.Seed)
	caller, resp.YourType = e.classifyCaller(ctx, caller, observable)
	if resp.YourType == yagomodel.PeerVirgin {
		return resp, nil
	}
	if observable {
		observationContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			callerObservationTimeout,
		)
		e.reachability.ObserveCaller(observationContext, caller, resp.YourType)
		cancel()
		e.acceptCallerNews(ctx, caller, resp.YourType)
	}
	resp.Seeds = append(
		[]yagomodel.Seed{e.status.SelfSeed(ctx)},
		e.knownPeers(ctx, req.Count)...,
	)

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
	observable bool,
) (yagomodel.Seed, yagomodel.PeerType) {
	if samePeerIdentity(caller, e.status.SelfSeed(ctx)) {
		return caller, yagomodel.PeerVirgin
	}

	if !observable {
		return caller, yagomodel.PeerJunior
	}

	verifiedCaller, reachable := e.probe.ReachableCaller(
		ctx,
		caller,
		e.status.SelfSeed(ctx).Hash,
		e.status.NetworkName(ctx),
	)
	if !reachable {
		return caller, yagomodel.PeerJunior
	}
	if peerType, known := caller.PeerType.Get(); known && peerType == yagomodel.PeerPrincipal {
		return verifiedCaller, yagomodel.PeerPrincipal
	}

	return verifiedCaller, yagomodel.PeerSenior
}

func samePeerIdentity(caller, self yagomodel.Seed) bool {
	return caller.Hash == self.Hash
}

func (e helloEndpoint) knownPeers(ctx context.Context, count int) []yagomodel.Seed {
	if count <= 0 {
		return nil
	}
	limit := min(count, maximumHelloKnownPeers)
	known := e.reachability.FreshestPeers(ctx, limit)

	return known[:min(limit, len(known))]
}
