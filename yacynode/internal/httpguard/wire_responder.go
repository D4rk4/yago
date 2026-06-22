package httpguard

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const wireContentType = "text/plain; charset=UTF-8"

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
	if _, err := io.WriteString(w, msg.Encode()); err != nil {
		slog.WarnContext(ctx, "wire response write failed", slog.Any("error", err))
	}
}
