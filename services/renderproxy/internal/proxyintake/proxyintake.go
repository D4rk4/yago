// Package proxyintake accepts absolute-URL forward-proxy requests and refuses tunnels.
package proxyintake

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/renderedpage"
)

const headerContentType = "Content-Type"

const msgRenderFailed = "render failed"

type Handler struct {
	renderer renderedpage.Renderer
}

func New(renderer renderedpage.Renderer) *Handler {
	return &Handler{renderer: renderer}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !r.URL.IsAbs() {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	page, err := h.renderer.Render(r.Context(), r.URL.String())
	if err != nil {
		h.writeFailure(w, r.Context(), err)
		return
	}

	if page.ContentType != "" {
		w.Header().Set(headerContentType, page.ContentType)
	}
	w.WriteHeader(page.StatusCode)
	_, _ = w.Write(page.Body)
}

func (h *Handler) writeFailure(w http.ResponseWriter, ctx context.Context, err error) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		w.WriteHeader(http.StatusGatewayTimeout)
		return
	}
	slog.WarnContext(ctx, msgRenderFailed, slog.Any("error", err))
	w.WriteHeader(http.StatusBadGateway)
}
