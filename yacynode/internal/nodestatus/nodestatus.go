// Package nodestatus owns the node's runtime status: its self-seed, the
// version/uptime header every endpoint echoes, and the query.html capacity
// answers. Its published port, Report, is the only surface other modules
// import. Live counts arrive through the RWICounter and URLCounter ports, so
// nodestatus never reads another module's schema.
package nodestatus

import (
	"context"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type Report interface {
	Version(ctx context.Context) string
	Uptime(ctx context.Context) int
	SelfSeed(ctx context.Context) yacymodel.Seed
}

type RWICounter interface {
	RWICount(ctx context.Context) (int, error)
	ReferencedURLCount(ctx context.Context) (int, error)
}

type URLCounter interface {
	Count(ctx context.Context) (int, error)
}

type Identity struct {
	Hash        yacymodel.Hash
	NetworkName string
	Name        string
	Host        string
	Port        int
	Flags       yacymodel.Flags
	Version     string
	Start       time.Time
}

func (id Identity) Uptime(now time.Time) int {
	return int(now.Sub(id.Start).Minutes())
}

func NewReport(id Identity, rwi RWICounter, urls URLCounter) Report {
	return newReport(id, time.Now, rwi, urls)
}

func MountQuery(
	router httpguard.WireRouter,
	peer httpguard.PeerIdentity,
	rwi RWICounter,
	urls URLCounter,
) {
	httpguard.Mount(
		router,
		yacyproto.PathQuery,
		yacyproto.QueryEndpointMethods,
		yacyproto.ParseQueryRequest,
		queryEndpoint{peer: peer, rwi: rwi, urls: urls}.Serve,
	)
}
