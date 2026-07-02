package httpguard

import (
	"context"
	"net/http"
	"net/url"

	"github.com/D4rk4/yago/yacyproto"
)

type WireRouter struct {
	mux  *http.ServeMux
	gate WireGate
}

func NewWireRouter(mux *http.ServeMux, gate WireGate) WireRouter {
	return WireRouter{mux: mux, gate: gate}
}

func Mount[Req any, Resp WireResponse](
	router WireRouter,
	path string,
	methods yacyproto.EndpointMethodSet,
	parse func(ctx context.Context, form url.Values) (Req, error),
	serve func(ctx context.Context, req Req) (Resp, error),
) {
	router.mux.Handle(path, Serve(router.gate, methods, parse, serve))
}
