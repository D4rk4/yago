// Package nodestatus owns the node's runtime status: its self-seed, the
// version/uptime header every endpoint echoes, and the query.html capacity
// answers. Its published port, Report, is the only surface other modules
// import. Live counts arrive through the RWICounter and URLCounter ports, so
// nodestatus never reads another module's schema.
package nodestatus

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

type Report interface {
	Version(ctx context.Context) string
	Uptime(ctx context.Context) int
	SelfSeed(ctx context.Context) yacymodel.Seed
}

type RWICounter interface {
	RWICount(ctx context.Context) (int, error)
	RWIURLCount(ctx context.Context, word yacymodel.Hash) (int, error)
}

type URLCounter interface {
	Count(ctx context.Context) (int, error)
}

type KnownPeerCounter interface {
	KnownPeerCount(ctx context.Context) int
}

func NewReport(
	id nodeidentity.Identity,
	rwi RWICounter,
	urls URLCounter,
	peers KnownPeerCounter,
) Report {
	return newReport(id, time.Now, rwi, urls, peers)
}

func MountQuery(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	rwi RWICounter,
	urls URLCounter,
) {
	httpguard.Mount(
		router,
		yacyproto.PathQuery,
		yacyproto.QueryEndpointMethods,
		yacyproto.ParseQueryRequest,
		queryEndpoint{identity: identity, rwi: rwi, urls: urls}.Serve,
	)
}
