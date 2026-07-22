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
	status      RuntimeStatus
	contentType string
}

func NewWireResponder(status RuntimeStatus) WireResponder {
	return WireResponder{status: status, contentType: wireContentType}
}

func (r WireResponder) Write(ctx context.Context, w http.ResponseWriter, msg yagomodel.Message) {
	yagoproto.InjectResponseHeader(msg, r.status.Version(ctx), r.status.Uptime(ctx))
	writeWireMessageWithContentType(ctx, w, msg, r.contentType)
}

func writeWireMessage(ctx context.Context, w http.ResponseWriter, msg yagomodel.Message) {
	writeWireMessageWithContentType(ctx, w, msg, wireContentType)
}
