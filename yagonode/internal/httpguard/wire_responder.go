package httpguard

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
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

func (r WireResponder) Write(ctx context.Context, w http.ResponseWriter, msg yagomodel.Message) {
	yagoproto.InjectResponseHeader(msg, r.status.Version(ctx), r.status.Uptime(ctx))
	writeWireMessage(ctx, w, msg)
}

func writeWireMessage(ctx context.Context, w http.ResponseWriter, msg yagomodel.Message) {
	w.Header().Set("Content-Type", wireContentType)
	if err := writeResponseText(w, msg.Encode()); err != nil {
		reportWireResponseWriteFailure(ctx, err)
	}
}
