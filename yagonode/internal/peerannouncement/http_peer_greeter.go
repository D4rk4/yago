package peerannouncement

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const greetMaxBodyBytes int64 = 256 << 10

var errGreetFailed = errors.New("peer greet failed")

type greetResult struct {
	YourIP   string
	YourType yagomodel.PeerType
	Known    []yagomodel.Seed
}

type httpPeerGreeter struct {
	client      *http.Client
	networkName string
}

var (
	newGreetRequest   = http.NewRequestWithContext
	parseGreetMessage = yagomodel.ParseMessage
)

func newHTTPPeerGreeter(client *http.Client, networkName string) httpPeerGreeter {
	return httpPeerGreeter{client: client, networkName: networkName}
}

func (g httpPeerGreeter) Greet(
	ctx context.Context,
	endpoint string,
	self yagomodel.Seed,
	count int,
) (greetResult, error) {
	target, err := greetURL(endpoint)
	if err != nil {
		return greetResult{}, err
	}

	request := yagoproto.HelloRequest{
		NetworkName: g.networkName,
		Seed:        self,
		Count:       count,
		Iam:         self.Hash,
	}

	req, err := newGreetRequest(
		ctx,
		http.MethodPost,
		target.String(),
		strings.NewReader(request.Form().Encode()),
	)
	if err != nil {
		return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client.Do(req)
	if err != nil {
		return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
	}
	defer closeResponseBody(ctx, resp.Body, "peerGreet")

	if resp.StatusCode != http.StatusOK {
		return greetResult{}, fmt.Errorf("%w: status %d", errGreetFailed, resp.StatusCode)
	}

	return parseGreetResponse(ctx, io.LimitReader(resp.Body, greetMaxBodyBytes))
}

func parseGreetResponse(ctx context.Context, body io.Reader) (greetResult, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
	}

	msg, err := parseGreetMessage(string(raw))
	if err != nil {
		return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
	}
	parsed, err := yagoproto.ParseHelloResponse(ctx, msg)
	if err != nil {
		return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
	}

	return greetResult{
		YourIP:   parsed.YourIP,
		YourType: parsed.YourType,
		Known:    parsed.KnownSeeds(),
	}, nil
}

func greetURL(endpoint string) (*url.URL, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("%w: empty endpoint", errGreetFailed)
	}

	return &url.URL{
		Scheme: "http",
		Host:   endpoint,
		Path:   yagoproto.PathHello,
	}, nil
}
