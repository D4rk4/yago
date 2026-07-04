package httpguard

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/D4rk4/yago/yagoproto"
)

const msgRawResponseWriteFailed = "raw response write failed"

type RawResponse struct {
	ContentType string
	Body        string
}

func MountRaw[Req any](
	router WireRouter,
	path string,
	methods yagoproto.EndpointMethodSet,
	parse func(ctx context.Context, form url.Values) (Req, error),
	serve func(ctx context.Context, req Req) (RawResponse, error),
) {
	router.mux.Handle(path, ServeRaw(router.gate, methods, parse, serve))
}

func ServeRaw[Req any](
	gate WireGate,
	methods yagoproto.EndpointMethodSet,
	parse func(ctx context.Context, form url.Values) (Req, error),
	serve func(ctx context.Context, req Req) (RawResponse, error),
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

		writeRawResponse(ctx, w, resp)
	})
}

func writeRawResponse(ctx context.Context, w http.ResponseWriter, resp RawResponse) {
	if resp.ContentType != "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	if err := writeResponseText(w, resp.Body); err != nil {
		slog.WarnContext(ctx, msgRawResponseWriteFailed, slog.Any("error", err))
	}
}
