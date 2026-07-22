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

type PeerDirectory interface {
	ReachablePeers(ctx context.Context) []yagomodel.Seed
	SeedlistPeers(ctx context.Context, limit int) []yagomodel.Seed
	PeerByHash(ctx context.Context, peer yagomodel.Hash) (yagomodel.Seed, bool)
	PeerByName(ctx context.Context, name string) (yagomodel.Seed, bool)
}

func Mount(
	router httpguard.WireRouter,
	status RuntimeStatus,
	peers PeerDirectory,
) {
	httpguard.MountRaw(
		router,
		yagoproto.PathSeedlist,
		yagoproto.SeedlistEndpointMethods,
		yagoproto.ParseSeedlistRequest,
		endpoint{status: status, peers: peers}.ServeHTML,
	)
	httpguard.MountRaw(
		router,
		yagoproto.PathSeedlistJSON,
		yagoproto.SeedlistEndpointMethods,
		yagoproto.ParseSeedlistRequest,
		endpoint{status: status, peers: peers}.ServeJSON,
	)
	httpguard.MountRaw(
		router,
		yagoproto.PathSeedlistXML,
		yagoproto.SeedlistEndpointMethods,
		yagoproto.ParseSeedlistRequest,
		endpoint{status: status, peers: peers}.ServeXML,
	)
	httpguard.MountRaw(
		router,
		yagoproto.PathP2PSeeds,
		yagoproto.SeedlistEndpointMethods,
		yagoproto.ParseSeedlistRequest,
		endpoint{status: status, peers: peers}.ServeHTML,
	)
	httpguard.MountRaw(
		router,
		yagoproto.PathP2PSeedsJSON,
		yagoproto.SeedlistEndpointMethods,
		yagoproto.ParseSeedlistRequest,
		endpoint{status: status, peers: peers}.ServeJSON,
	)
}
