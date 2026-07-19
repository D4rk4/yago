package peeradmission

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	backPingMaxBodyBytes                 int64 = 64 << 10
	callerBackPingUnreachableMessage           = "caller back-ping unreachable"
	callerBackPingBodyCloseFailedMessage       = "caller back-ping body close failed"
)

type callerBackPing struct {
	client      *http.Client
	preferHTTPS bool
	access      yagoproto.NetworkAccess
	signForm    func(yagoproto.NetworkAccess, url.Values) error
}

func newCallerBackPing(
	client *http.Client,
	preferHTTPS bool,
	access ...yagoproto.NetworkAccess,
) callerBackPing {
	var configured yagoproto.NetworkAccess
	if len(access) != 0 {
		configured = access[0]
	}

	return callerBackPing{
		client: client, preferHTTPS: preferHTTPS, access: configured,
		signForm: yagoproto.NetworkAccess.Sign,
	}
}

var _ callerReachabilityProbe = callerBackPing{}

var parseBackPingMessage = yagomodel.ParseMessage

func (p callerBackPing) Reachable(
	ctx context.Context,
	caller yagomodel.Seed,
	self yagomodel.Hash,
	networkName string,
) bool {
	targets, err := caller.ProtocolEndpoints(yagoproto.PathQuery, p.preferHTTPS)
	if err != nil {
		slog.DebugContext(ctx, callerBackPingUnreachableMessage, slog.Any("error", err))

		return false
	}

	query := yagoproto.QueryRequest{
		NetworkName: networkName,
		YouAre:      caller.Hash,
		Iam:         self,
		Object:      yagoproto.ObjectRWICount,
	}
	form := query.Form()
	if p.access.Mode == yagoproto.NetworkAuthenticationSaltedMagic {
		access := p.access
		access.NetworkName = networkName
		access.Self = self
		if err := p.signForm(access, form); err != nil {
			slog.DebugContext(ctx, callerBackPingUnreachableMessage, slog.Any("error", err))

			return false
		}
	}
	rawQuery := form.Encode()

	// A reachability probe passes when any candidate scheme answers, so walk
	// the https-first candidates until one connects.
	for _, target := range targets {
		target.RawQuery = rawQuery
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		resp, err := p.client.Do(req)
		if err != nil {
			slog.DebugContext(ctx, callerBackPingUnreachableMessage, slog.Any("error", err))

			continue
		}

		return p.confirmClose(ctx, resp)
	}

	return false
}

func (p callerBackPing) confirmClose(ctx context.Context, resp *http.Response) bool {
	defer p.close(ctx, resp.Body)

	return p.confirms(ctx, resp)
}

func (p callerBackPing) confirms(ctx context.Context, resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK {
		slog.DebugContext(
			ctx,
			callerBackPingUnreachableMessage,
			slog.Int("status", resp.StatusCode),
		)

		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, backPingMaxBodyBytes))
	if err != nil {
		slog.DebugContext(ctx, callerBackPingUnreachableMessage, slog.Any("error", err))

		return false
	}

	msg, err := parseBackPingMessage(string(body))
	if err != nil {
		slog.DebugContext(ctx, callerBackPingUnreachableMessage, slog.Any("error", err))

		return false
	}
	if _, err := yagoproto.ParseQueryResponse(msg); err != nil {
		slog.DebugContext(ctx, callerBackPingUnreachableMessage, slog.Any("error", err))

		return false
	}

	return true
}

func (p callerBackPing) close(ctx context.Context, body io.Closer) {
	if err := body.Close(); err != nil {
		slog.WarnContext(ctx, callerBackPingBodyCloseFailedMessage, slog.Any("error", err))
	}
}
