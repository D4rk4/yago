package peerprofile

import (
	"context"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

type Property struct {
	Key   string
	Value string
}

type Properties interface {
	Properties(ctx context.Context) []Property
}

type NoPeerProfile struct{}

func (NoPeerProfile) Properties(context.Context) []Property {
	return nil
}

func Mount(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	profile Properties,
) {
	httpguard.MountRaw(
		router,
		yacyproto.PathProfile,
		yacyproto.ProfileEndpointMethods,
		yacyproto.ParseProfileRequest,
		endpoint{identity: identity, profile: profile}.Serve,
	)
}
