// Package nodestatus owns the node's runtime status: its self-seed, the
// version/uptime header every endpoint echoes, and the query.html capacity
// answers. Its published port, Report, is the only surface other modules
// import. Live counts arrive through the RWICounter and URLCounter ports, so
// nodestatus never reads another module's schema.
package nodestatus

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
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
}
