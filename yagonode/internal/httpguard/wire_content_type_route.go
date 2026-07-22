package httpguard

import (
	"context"
	"net/http"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type WireEndpoint[Req any, Resp WireResponse] struct {
	Parse func(ctx context.Context, form url.Values) (Req, error)
	Serve func(ctx context.Context, req Req) (Resp, error)
}

func (r WireResponder) WithContentType(contentType string) WireResponder {
	r.contentType = contentType

	return r
}

func writeWireMessageWithContentType(
	ctx context.Context,
	w http.ResponseWriter,
	msg yagomodel.Message,
	contentType string,
) {
	w.Header().Set("Content-Type", contentType)
	if err := writeResponseText(w, msg.Encode()); err != nil {
		reportWireResponseWriteFailure(ctx, err)
	}
}

func MountWithContentType[Req any, Resp WireResponse](
	router WireRouter,
	path string,
	methods yagoproto.EndpointMethodSet,
	contentType string,
	endpoint WireEndpoint[Req, Resp],
) {
	gate := router.gate
	gate.Respond = gate.Respond.WithContentType(contentType)
	router.mux.Handle(path, Serve(gate, methods, endpoint.Parse, endpoint.Serve))
}
