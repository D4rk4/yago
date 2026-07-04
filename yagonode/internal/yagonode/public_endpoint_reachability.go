package yagonode

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const publicSelfTestMaxBodyBytes int64 = 64 << 10

type publicReachability interface {
	Reachable(ctx context.Context) bool
}

type publicEndpointSelfTest struct {
	client      *http.Client
	networkName string
	self        yagomodel.Hash
	base        *url.URL
}

func newPublicEndpointSelfTest(
	client *http.Client,
	networkName string,
	self yagomodel.Hash,
	base *url.URL,
) publicEndpointSelfTest {
	if client == nil {
		client = http.DefaultClient
	}

	return publicEndpointSelfTest{
		client:      client,
		networkName: networkName,
		self:        self,
		base:        base,
	}
}

func (p publicEndpointSelfTest) Reachable(ctx context.Context) bool {
	if p.base == nil {
		return false
	}

	target := p.queryURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		slog.DebugContext(ctx, "public endpoint self-test failed", slog.Any("error", err))

		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		slog.DebugContext(ctx, "public endpoint self-test failed", slog.Any("error", err))

		return false
	}
	defer func() { _ = resp.Body.Close() }()

	return p.confirm(ctx, resp)
}

func (p publicEndpointSelfTest) queryURL() url.URL {
	target := *p.base
	target.Path = joinPublicSelfTestPath(p.base.Path, yagoproto.PathQuery)
	target.RawQuery = yagoproto.QueryRequest{
		NetworkName: p.networkName,
		YouAre:      p.self,
		Object:      yagoproto.ObjectRWICount,
	}.Form().Encode()

	return target
}

func joinPublicSelfTestPath(prefix, path string) string {
	return strings.TrimRight(prefix, "/") + path
}

func (p publicEndpointSelfTest) confirm(ctx context.Context, resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK {
		slog.DebugContext(
			ctx,
			"public endpoint self-test failed",
			slog.Int("status", resp.StatusCode),
		)

		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, publicSelfTestMaxBodyBytes))
	if err != nil {
		slog.DebugContext(ctx, "public endpoint self-test failed", slog.Any("error", err))

		return false
	}

	msg, _ := yagomodel.ParseMessage(string(body))
	parsed, err := yagoproto.ParseQueryResponse(msg)
	if err != nil {
		slog.DebugContext(ctx, "public endpoint self-test failed", slog.Any("error", err))

		return false
	}
	if parsed.Response == yagoproto.QueryResponseRejected {
		slog.DebugContext(ctx, "public endpoint self-test rejected")

		return false
	}
	if parsed.Response < 0 {
		slog.DebugContext(
			ctx,
			"public endpoint self-test failed",
			slog.Int("response", parsed.Response),
		)

		return false
	}

	return true
}
