package httpguard

import (
	"context"
	"net/url"

	"github.com/D4rk4/yago/yagoproto"
)

type RawRouteAdmission[Req any] struct {
	Path      string
	Methods   yagoproto.EndpointMethodSet
	Parse     func(ctx context.Context, form url.Values) (Req, error)
	Serve     func(ctx context.Context, req Req) (RawResponse, error)
	Admission *IntakeGate
}
