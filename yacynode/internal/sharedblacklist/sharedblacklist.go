package sharedblacklist

import (
	"context"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
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
		yacyproto.PathList,
		yacyproto.ListEndpointMethods,
		yacyproto.ParseListRequest,
		endpoint{networkName: networkName, blacklists: blacklists}.Serve,
	)
}
