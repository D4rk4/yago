package seedlist

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type RuntimeStatus interface {
	SelfSeed(ctx context.Context) yagomodel.Seed
}

type ReachablePeers interface {
	ReachablePeers(ctx context.Context) []yagomodel.Seed
}

func Mount(
	router httpguard.WireRouter,
	status RuntimeStatus,
	reachability ReachablePeers,
) {
	httpguard.MountRaw(
		router,
		yagoproto.PathSeedlist,
		yagoproto.SeedlistEndpointMethods,
		yagoproto.ParseSeedlistRequest,
		endpoint{status: status, reachability: reachability}.ServeHTML,
	)
	httpguard.MountRaw(
		router,
		yagoproto.PathSeedlistJSON,
		yagoproto.SeedlistEndpointMethods,
		yagoproto.ParseSeedlistRequest,
		endpoint{status: status, reachability: reachability}.ServeJSON,
	)
	httpguard.MountRaw(
		router,
		yagoproto.PathSeedlistXML,
		yagoproto.SeedlistEndpointMethods,
		yagoproto.ParseSeedlistRequest,
		endpoint{status: status, reachability: reachability}.ServeXML,
	)
}
