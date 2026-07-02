package sharedblacklist

import (
	"context"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

type Blacklists interface {
	Entries(ctx context.Context, name string) []string
}

type NoSharedBlacklists struct{}

func (NoSharedBlacklists) Entries(context.Context, string) []string {
	return nil
}

func Mount(router httpguard.WireRouter, blacklists Blacklists) {
	httpguard.MountRaw(
		router,
		yacyproto.PathList,
		yacyproto.ListEndpointMethods,
		yacyproto.ParseListRequest,
		endpoint{blacklists: blacklists}.Serve,
	)
}
