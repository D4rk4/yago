package nodestatus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
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
	peer httpguard.PeerIdentity
	rwi  RWICounter
	urls URLCounter
}

func (e queryEndpoint) Serve(
	ctx context.Context,
	req yacyproto.QueryRequest,
) (yacyproto.QueryResponse, error) {
	resp := yacyproto.QueryResponse{Response: yacyproto.QueryResponseRejected}

	if e.peer.NetworkMatches(req.NetworkName) && e.peer.YouAreMatches(req.YouAre) {
		count, supported, err := e.count(ctx, req.Object)
		if err != nil {
			return yacyproto.QueryResponse{}, fmt.Errorf("%s: %w", msgCountFailed, err)
		}
		if supported {
			resp.Response = count
		}
	}

	slog.DebugContext(ctx, msgCountServed,
		slog.String("object", string(req.Object)),
		slog.Int("count", resp.Response),
	)

	return resp, nil
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
