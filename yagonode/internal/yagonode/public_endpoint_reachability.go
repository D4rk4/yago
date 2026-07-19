package yagonode

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	publicSelfTestMaxBodyBytes            int64 = 64 << 10
	publicEndpointSelfTestFailedMessage         = "public endpoint self-test failed"
	publicEndpointSelfTestRejectedMessage       = "public endpoint self-test rejected"
)

type publicReachability interface {
	Reachable(ctx context.Context) bool
}

type publicEndpointSelfTest struct {
	client      *http.Client
	networkName string
	self        yagomodel.Hash
	base        *url.URL
	access      yagoproto.NetworkAccess
	sign        func(url.Values) error
}

func newPublicEndpointSelfTest(
	client *http.Client,
	networkName string,
	self yagomodel.Hash,
	base *url.URL,
	access ...yagoproto.NetworkAccess,
) publicEndpointSelfTest {
	if client == nil {
		client = http.DefaultClient
	}

	configured := yagoproto.NetworkAccess{NetworkName: networkName, Self: self}
	if len(access) != 0 {
		configured = access[0]
		configured.Self = self
	}

	return publicEndpointSelfTest{
		client:      client,
		networkName: networkName,
		self:        self,
		base:        base,
		access:      configured,
		sign:        configured.Sign,
	}
}

func (p publicEndpointSelfTest) Reachable(ctx context.Context) bool {
	if p.base == nil {
		return false
	}

	target, err := p.queryURL()
	if err != nil {
		slog.DebugContext(ctx, publicEndpointSelfTestFailedMessage, slog.Any("error", err))

		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		slog.DebugContext(ctx, publicEndpointSelfTestFailedMessage, slog.Any("error", err))

		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		slog.DebugContext(ctx, publicEndpointSelfTestFailedMessage, slog.Any("error", err))

		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return p.confirm(ctx, resp)
}

func (p publicEndpointSelfTest) queryURL() (url.URL, error) {
	target := *p.base
	target.Path = joinPublicSelfTestPath(p.base.Path, yagoproto.PathQuery)
	form := yagoproto.QueryRequest{
		NetworkName: p.networkName,
		YouAre:      p.self,
		Object:      yagoproto.ObjectRWICount,
	}.Form()
	if p.access.Mode == yagoproto.NetworkAuthenticationSaltedMagic {
		if err := p.sign(form); err != nil {
			return url.URL{}, fmt.Errorf("sign public endpoint self-test: %w", err)
		}
	}
	target.RawQuery = form.Encode()

	return target, nil
}

func joinPublicSelfTestPath(prefix, path string) string {
	return strings.TrimRight(prefix, "/") + path
}

func (p publicEndpointSelfTest) confirm(ctx context.Context, resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK {
		slog.DebugContext(
			ctx,
			publicEndpointSelfTestFailedMessage,
			slog.Int("status", resp.StatusCode),
		)

		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, publicSelfTestMaxBodyBytes))
	if err != nil {
		slog.DebugContext(ctx, publicEndpointSelfTestFailedMessage, slog.Any("error", err))

		return false
	}

	msg, _ := yagomodel.ParseMessage(string(body))
	parsed, err := yagoproto.ParseQueryResponse(msg)
	if err != nil {
		slog.DebugContext(ctx, publicEndpointSelfTestFailedMessage, slog.Any("error", err))

		return false
	}
	if parsed.Response == yagoproto.QueryResponseRejected {
		slog.DebugContext(ctx, publicEndpointSelfTestRejectedMessage)

		return false
	}
	if parsed.Response < 0 {
		slog.DebugContext(
			ctx,
			publicEndpointSelfTestFailedMessage,
			slog.Int("response", parsed.Response),
		)

		return false
	}

	return true
}
