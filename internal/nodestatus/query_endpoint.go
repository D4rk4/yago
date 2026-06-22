package nodestatus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const (
	msgCountFailed = "count failed"
	msgCountServed = "count served"
)

var (
	errRWICount    = errors.New("count stored RWI words")
	errRWIURLCount = errors.New("count URLs referenced by stored RWI")
	errLURLCount   = errors.New("count stored URL metadata records")
)

type queryEndpoint struct {
	guard  httpguard.RequestGuard
	report Report
	rwi    RWICounter
	urls   URLCounter
}

func (e queryEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := e.guard.Parse(w, r, yacyproto.QueryEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseQueryRequest(form)
	if err != nil {
		httpguard.FailBadRequest(ctx, w, err)

		return
	}

	resp := yacyproto.QueryResponse{
		ResponseHeader: e.report.Header(ctx),
		Response:       yacyproto.QueryResponseRejected,
	}

	if e.guard.NetworkMatches(form) && e.guard.YouAreMatches(req.YouAre) {
		count, supported, err := e.count(ctx, req.Object)
		if err != nil {
			httpguard.FailInternal(ctx, w, msgCountFailed, err)

			return
		}
		if supported {
			resp.Response = count
		}
	}

	slog.DebugContext(ctx, msgCountServed,
		slog.String("object", string(req.Object)),
		slog.Int("count", resp.Response),
	)
	httpguard.WriteWireMessage(ctx, w, resp.Encode())
}

func (e queryEndpoint) count(ctx context.Context, object yacyproto.QueryObject) (int, bool, error) {
	switch object {
	case yacyproto.ObjectRWICount:
		n, err := e.rwi.RWICount(ctx)

		return n, true, wrapCount(errRWICount, err)
	case yacyproto.ObjectRWIURLCount:
		n, err := e.rwi.ReferencedURLCount(ctx)

		return n, true, wrapCount(errRWIURLCount, err)
	case yacyproto.ObjectLURLCount:
		n, err := e.urls.Count(ctx)

		return n, true, wrapCount(errLURLCount, err)
	default:
		return 0, false, nil
	}
}

func wrapCount(sentinel error, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w: %w", sentinel, err)
}
