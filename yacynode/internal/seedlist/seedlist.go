package seedlist

import (
	"context"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

type RuntimeStatus interface {
	SelfSeed(ctx context.Context) yacymodel.Seed
}

type ReachablePeers interface {
	ReachablePeers(ctx context.Context) []yacymodel.Seed
}

func Mount(
	router httpguard.WireRouter,
	status RuntimeStatus,
	reachability ReachablePeers,
) {
	httpguard.MountRaw(
		router,
		yacyproto.PathSeedlist,
		yacyproto.SeedlistEndpointMethods,
		yacyproto.ParseSeedlistRequest,
		endpoint{status: status, reachability: reachability}.ServeHTML,
	)
	httpguard.MountRaw(
		router,
		yacyproto.PathSeedlistJSON,
		yacyproto.SeedlistEndpointMethods,
		yacyproto.ParseSeedlistRequest,
		endpoint{status: status, reachability: reachability}.ServeJSON,
	)
	httpguard.MountRaw(
		router,
		yacyproto.PathSeedlistXML,
		yacyproto.SeedlistEndpointMethods,
		yacyproto.ParseSeedlistRequest,
		endpoint{status: status, reachability: reachability}.ServeXML,
	)
}
