package peeradmission

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const backPingMaxBodyBytes int64 = 64 << 10

type callerBackPing struct {
	client *http.Client
}

func newCallerBackPing(client *http.Client) callerBackPing {
	return callerBackPing{client: client}
}

var _ callerReachabilityProbe = callerBackPing{}

func (p callerBackPing) Reachable(
	ctx context.Context,
	caller yacymodel.Seed,
	self yacymodel.Hash,
	networkName string,
) bool {
	target, err := caller.HTTPEndpoint(yacyproto.PathQuery)
	if err != nil {
		slog.DebugContext(ctx, "caller back-ping unreachable", slog.Any("error", err))

		return false
	}

	query := yacyproto.QueryRequest{
		NetworkName: networkName,
		YouAre:      caller.Hash,
		Iam:         self,
		Object:      yacyproto.ObjectRWICount,
	}
	target.RawQuery = query.Form().Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		slog.DebugContext(ctx, "caller back-ping unreachable", slog.Any("error", err))

		return false
	}

	resp, err := p.client.Do(req)
	if err != nil {
		slog.DebugContext(ctx, "caller back-ping unreachable", slog.Any("error", err))

		return false
	}
	defer p.close(ctx, resp.Body)

	return p.confirms(ctx, resp)
}

func (p callerBackPing) confirms(ctx context.Context, resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK {
		slog.DebugContext(ctx, "caller back-ping unreachable", slog.Int("status", resp.StatusCode))

		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, backPingMaxBodyBytes))
	if err != nil {
		slog.DebugContext(ctx, "caller back-ping unreachable", slog.Any("error", err))

		return false
	}

	msg, err := yacymodel.ParseMessage(string(body))
	if err != nil {
		slog.DebugContext(ctx, "caller back-ping unreachable", slog.Any("error", err))

		return false
	}
	if _, err := yacyproto.ParseQueryResponse(msg); err != nil {
		slog.DebugContext(ctx, "caller back-ping unreachable", slog.Any("error", err))

		return false
	}

	return true
}

func (p callerBackPing) close(ctx context.Context, body io.Closer) {
	if err := body.Close(); err != nil {
		slog.WarnContext(ctx, "caller back-ping body close failed", slog.Any("error", err))
	}
}
