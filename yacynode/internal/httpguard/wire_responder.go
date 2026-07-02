package httpguard

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

const wireContentType = "text/plain; charset=UTF-8"

const msgWireResponseWriteFailed = "wire response write failed"

type RuntimeStatus interface {
	Version(ctx context.Context) string
	Uptime(ctx context.Context) int
}

type WireResponder struct {
	status RuntimeStatus
}

func NewWireResponder(status RuntimeStatus) WireResponder {
	return WireResponder{status: status}
}

func (r WireResponder) Write(ctx context.Context, w http.ResponseWriter, msg yacymodel.Message) {
	yacyproto.InjectResponseHeader(msg, r.status.Version(ctx), r.status.Uptime(ctx))
	writeWireMessage(ctx, w, msg)
}

func writeWireMessage(ctx context.Context, w http.ResponseWriter, msg yacymodel.Message) {
	w.Header().Set("Content-Type", wireContentType)
	if err := writeResponseText(w, msg.Encode()); err != nil {
		slog.WarnContext(ctx, msgWireResponseWriteFailed, slog.Any("error", err))
	}
}
