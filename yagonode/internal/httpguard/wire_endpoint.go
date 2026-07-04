package httpguard

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type WireResponse interface {
	Encode() yagomodel.Message
}

type WireGate struct {
	Guard   RequestGuard
	Respond WireResponder
	Address ClientAddressResolver
}

func Serve[Req any, Resp WireResponse](
	gate WireGate,
	methods yagoproto.EndpointMethodSet,
	parse func(ctx context.Context, form url.Values) (Req, error),
	serve func(ctx context.Context, req Req) (Resp, error),
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form, ctx, cancel, ok := gate.Guard.Parse(w, r, methods)
		if !ok {
			return
		}
		defer cancel()

		ctx = WithRemoteAddr(ctx, gate.Address.Resolve(r))

		req, err := parse(ctx, form)
		if err != nil {
			FailBadRequest(ctx, w, err)

			return
		}

		resp, err := serve(ctx, req)
		if err != nil {
			failInternal(ctx, w, r.URL.Path, err)

			return
		}

		gate.Respond.Write(ctx, w, resp.Encode())
	})
}

func failInternal(ctx context.Context, w http.ResponseWriter, path string, err error) {
	slog.ErrorContext(ctx, "request failed",
		slog.String("path", path),
		slog.Any("error", err),
	)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
