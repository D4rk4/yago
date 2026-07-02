package nodestatus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

const (
	msgCountFailed = "count failed"
	msgCountServed = "count served"
)

var (
	errRWICount    = errors.New("count stored RWI words")
	errRWIURLCount = errors.New("count URLs for stored RWI word")
	errLURLCount   = errors.New("count stored URL metadata records")
)

type queryEndpoint struct {
	identity nodeidentity.Identity
	rwi      RWICounter
	urls     URLCounter
}

func (e queryEndpoint) Serve(
	ctx context.Context,
	req yacyproto.QueryRequest,
) (yacyproto.QueryResponse, error) {
	resp := yacyproto.QueryResponse{Response: yacyproto.QueryResponseRejected}

	if e.identity.Addresses(req.NetworkName, req.YouAre) {
		count, supported, err := e.count(ctx, req)
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

func (e queryEndpoint) count(ctx context.Context, req yacyproto.QueryRequest) (int, bool, error) {
	switch req.Object {
	case yacyproto.ObjectRWICount:
		n, err := e.rwi.RWICount(ctx)

		return n, true, wrapCount(errRWICount, err)
	case yacyproto.ObjectRWIURLCount:
		word, ok := queryWordHash(req.Env)
		if !ok {
			return 0, true, nil
		}

		n, err := e.rwi.RWIURLCount(ctx, word)

		return n, true, wrapCount(errRWIURLCount, err)
	case yacyproto.ObjectLURLCount:
		n, err := e.urls.Count(ctx)

		return n, true, wrapCount(errLURLCount, err)
	case yacyproto.ObjectWantedLURLs,
		yacyproto.ObjectWantedPURLs,
		yacyproto.ObjectWantedWord,
		yacyproto.ObjectWantedRWI,
		yacyproto.ObjectWantedSeeds:
		return 0, true, nil
	default:
		return 0, false, nil
	}
}

func queryWordHash(raw string) (yacymodel.Hash, bool) {
	if len(raw) != yacymodel.HashLength {
		return "", false
	}

	return yacymodel.Hash(raw), true
}

func wrapCount(sentinel error, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w: %w", sentinel, err)
}
