package nodestatus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	msgCountFailed = "count failed"
	msgCountServed = "count served"
)

var (
	errRWICount    = errors.New("count stored RWI words")
	errRWIURLCount = errors.New("count URLs for stored RWI word")
)

type queryEndpoint struct {
	identity nodeidentity.Identity
	rwi      RWICounter
	now      func() time.Time
}

func (e queryEndpoint) Serve(
	ctx context.Context,
	req yagoproto.QueryRequest,
) (yagoproto.QueryResponse, error) {
	resp := yagoproto.QueryResponse{
		Response: yagoproto.QueryResponseRejected,
		Magic:    queryMagic(e.identity),
	}

	if e.identity.Authenticates(
		req.NetworkName,
		req.NetworkNamePresent,
		req.Key,
		req.Iam,
		req.MagicMD5,
	) {
		resp.MyTime = e.currentTime().UTC().Format(queryTimeLayout)
		if req.YouAre != e.identity.Hash.String() {
			return resp, nil
		}
		count, supported, err := e.count(ctx, req)
		if err != nil {
			return yagoproto.QueryResponse{}, fmt.Errorf("%s: %w", msgCountFailed, err)
		}
		if supported {
			resp.Response = count
		} else {
			resp.UnresolvedResponse = true
		}
	}

	slog.DebugContext(ctx, msgCountServed,
		slog.String("object", string(req.Object)),
		slog.Int("count", resp.Response),
	)

	return resp, nil
}

func (e queryEndpoint) count(ctx context.Context, req yagoproto.QueryRequest) (int, bool, error) {
	switch req.Object {
	case yagoproto.ObjectRWICount:
		n, err := e.rwi.RWICount(ctx)

		return n, true, wrapCount(errRWICount, err)
	case yagoproto.ObjectRWIURLCount:
		word, ok := queryWordHash(req.Env)
		if !ok {
			return 0, true, nil
		}

		n, err := e.rwi.RWIURLCount(ctx, word)
		if err != nil {
			slog.WarnContext(ctx, msgCountFailed,
				slog.String("object", string(req.Object)),
				slog.Any("error", wrapCount(errRWIURLCount, err)),
			)

			return 0, false, nil
		}

		return n, true, nil
	case yagoproto.ObjectLURLCount:
		return 1, true, nil
	case yagoproto.ObjectWantedLURLs,
		yagoproto.ObjectWantedPURLs,
		yagoproto.ObjectWantedWord,
		yagoproto.ObjectWantedRWI,
		yagoproto.ObjectWantedSeeds:
		return 0, true, nil
	default:
		return 0, false, nil
	}
}

func queryWordHash(raw string) (yagomodel.Hash, bool) {
	if len(raw) != yagomodel.HashLength {
		return "", false
	}

	return yagomodel.Hash(raw), true
}

func wrapCount(sentinel error, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w: %w", sentinel, err)
}
