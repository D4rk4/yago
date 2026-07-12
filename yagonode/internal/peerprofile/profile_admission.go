package peerprofile

import (
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

const maximumConcurrentProfileRequests = 4

func mountWithAdmission(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	profile Properties,
	admission *httpguard.IntakeGate,
) {
	if profile == nil {
		profile = NoPeerProfile{}
	}
	httpguard.MountRawWithAdmission(
		router,
		httpguard.RawRouteAdmission[yagoproto.ProfileRequest]{
			Path:      yagoproto.PathProfile,
			Methods:   yagoproto.ProfileEndpointMethods,
			Parse:     yagoproto.ParseProfileRequest,
			Serve:     endpoint{identity: identity, profile: profile}.Serve,
			Admission: admission,
		},
	)
}
