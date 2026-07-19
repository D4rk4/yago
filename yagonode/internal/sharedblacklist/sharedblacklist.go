package sharedblacklist

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type Blacklists interface {
	SharedList(ctx context.Context, name string) string
}

type NoSharedBlacklists struct{}

func (NoSharedBlacklists) SharedList(context.Context, string) string {
	return ""
}

func Mount(router httpguard.WireRouter, identity nodeidentity.Identity, blacklists Blacklists) {
	httpguard.MountRawWithAdmission(
		router,
		httpguard.RawRouteAdmission[yagoproto.ListRequest]{
			Path:      yagoproto.PathList,
			Methods:   yagoproto.ListEndpointMethods,
			Parse:     yagoproto.ParseListRequest,
			Serve:     endpoint{identity: identity, blacklists: blacklists}.Serve,
			Admission: httpguard.NewIntakeGate(maximumConcurrentSharedBlacklist),
		},
	)
}
