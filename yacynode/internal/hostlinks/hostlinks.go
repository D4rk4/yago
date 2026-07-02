package hostlinks

import (
	"context"
	"encoding/json"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

type RuntimeStatus interface {
	Version(ctx context.Context) string
	Uptime(ctx context.Context) int
}

type IncomingHostLinks interface {
	IncomingHostLinks(ctx context.Context) Graph
}

type Graph struct {
	RowDefinition string
	LinkedHosts   []LinkedHost
}

type LinkedHost struct {
	HostHash   string
	References []json.RawMessage
}

type NoIncomingHostLinks struct{}

func (NoIncomingHostLinks) IncomingHostLinks(context.Context) Graph {
	return Graph{}
}

func Mount(
	router httpguard.WireRouter,
	networkName string,
	status RuntimeStatus,
	links IncomingHostLinks,
) {
	httpguard.MountRaw(
		router,
		yacyproto.PathIndex,
		yacyproto.IndexEndpointMethods,
		yacyproto.ParseIndexRequest,
		endpoint{networkName: networkName, status: status, links: links}.Serve,
	)
}
