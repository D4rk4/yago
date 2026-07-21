package peeradmission

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	backPingMaxBodyBytes                 int64 = 64 << 10
	callerBackPingUnreachableMessage           = "caller back-ping unreachable"
	callerBackPingBodyCloseFailedMessage       = "caller back-ping body close failed"
	callerBackPingHTTPTimeout                  = 6500 * time.Millisecond
	callerBackPingHTTPSTimeout                 = 13 * time.Second
)

type callerBackPing struct {
	client      *http.Client
	preferHTTPS bool
	access      yagoproto.NetworkAccess
	signForm    func(yagoproto.NetworkAccess, url.Values) error
	timeout     time.Duration
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

	timeout := callerBackPingHTTPTimeout
	if preferHTTPS {
		timeout = callerBackPingHTTPSTimeout
	}

	return callerBackPing{
		client: client, preferHTTPS: preferHTTPS, access: configured,
		signForm: yagoproto.NetworkAccess.Sign, timeout: timeout,
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
	probeContext, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
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
		req, _ := http.NewRequestWithContext(
			probeContext,
			http.MethodGet,
			target.String(),
			nil,
		)
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
	parsed, err := yagoproto.ParseQueryResponse(msg)
	if err != nil {
		slog.DebugContext(ctx, callerBackPingUnreachableMessage, slog.Any("error", err))

		return false
	}
	if parsed.Response < 0 {
		slog.DebugContext(
			ctx,
			callerBackPingUnreachableMessage,
			slog.Int("response", parsed.Response),
		)

		return false
	}

	return true
}

func (p callerBackPing) close(ctx context.Context, body io.Closer) {
	if err := body.Close(); err != nil {
		slog.WarnContext(ctx, callerBackPingBodyCloseFailedMessage, slog.Any("error", err))
	}
}
