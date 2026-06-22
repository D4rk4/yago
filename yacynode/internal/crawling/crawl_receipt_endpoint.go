package crawling

import (
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type crawlReceiptEndpoint struct {
	guard   httpguard.RequestGuard
	respond httpguard.WireResponder
}

func (e crawlReceiptEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, ctx, cancel, ok := e.guard.Parse(w, r, yacyproto.CrawlReceiptEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	slog.DebugContext(ctx, "crawl receipt rejected")
	e.respond.Write(ctx, w, yacyproto.CrawlReceiptResponse{}.Encode())
}
