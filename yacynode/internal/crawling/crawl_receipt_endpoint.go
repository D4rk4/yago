package crawling

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type StatusSnapshot struct {
	Version string
	Uptime  int
}

type RuntimeStatus interface {
	Snapshot(ctx context.Context) StatusSnapshot
}

type crawlReceiptEndpoint struct {
	guard  httpguard.RequestGuard
	status RuntimeStatus
}

func (e crawlReceiptEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, ctx, cancel, ok := e.guard.Parse(w, r, yacyproto.CrawlReceiptEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	snapshot := e.status.Snapshot(ctx)
	resp := yacyproto.CrawlReceiptResponse{
		ResponseHeader: yacyproto.ResponseHeader{
			Version: snapshot.Version,
			Uptime:  snapshot.Uptime,
		},
	}

	slog.DebugContext(ctx, "crawl receipt rejected")
	httpguard.WriteWireMessage(ctx, w, resp.Encode())
}
