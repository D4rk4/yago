package sharedblacklist

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type Blacklists interface {
	SharedList(ctx context.Context, name string) string
}

type NoSharedBlacklists struct{}

func (NoSharedBlacklists) SharedList(context.Context, string) string {
	return ""
}

func Mount(router httpguard.WireRouter, networkName string, blacklists Blacklists) {
	httpguard.MountRaw(
		router,
		yagoproto.PathList,
		yagoproto.ListEndpointMethods,
		yagoproto.ParseListRequest,
		endpoint{networkName: networkName, blacklists: blacklists}.Serve,
	)
}
