package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const greetMaxBodyBytes int64 = 256 << 10

var ErrGreetFailed = errors.New("peer greet failed")

type httpPeerGreeter struct {
	client      *http.Client
	networkName string
}

func newHTTPPeerGreeter(client *http.Client, networkName string) httpPeerGreeter {
	return httpPeerGreeter{client: client, networkName: networkName}
}

func (g httpPeerGreeter) Greet(
	ctx context.Context,
	endpoint string,
	self yacymodel.Seed,
	count int,
) (GreetResult, error) {
	target, err := greetURL(endpoint)
	if err != nil {
		return GreetResult{}, err
	}

	request := yacyproto.HelloRequest{
		NetworkName: g.networkName,
		Seed:        self,
		Count:       count,
		Iam:         self.Hash,
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		target.String(),
		strings.NewReader(request.Form().Encode()),
	)
	if err != nil {
		return GreetResult{}, fmt.Errorf("%w: %w", ErrGreetFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client.Do(req)
	if err != nil {
		return GreetResult{}, fmt.Errorf("%w: %w", ErrGreetFailed, err)
	}
	defer closeResponseBody(ctx, resp.Body, "peerGreet")

	if resp.StatusCode != http.StatusOK {
		return GreetResult{}, fmt.Errorf("%w: status %d", ErrGreetFailed, resp.StatusCode)
	}

	return parseGreetResponse(ctx, io.LimitReader(resp.Body, greetMaxBodyBytes))
}

func parseGreetResponse(ctx context.Context, body io.Reader) (GreetResult, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return GreetResult{}, fmt.Errorf("%w: %w", ErrGreetFailed, err)
	}

	msg, err := yacymodel.ParseMessage(string(raw))
	if err != nil {
		return GreetResult{}, fmt.Errorf("%w: %w", ErrGreetFailed, err)
	}
	parsed, err := yacyproto.ParseHelloResponse(ctx, msg)
	if err != nil {
		return GreetResult{}, fmt.Errorf("%w: %w", ErrGreetFailed, err)
	}

	return GreetResult{
		YourIP:   parsed.YourIP,
		YourType: parsed.YourType,
		Known:    parsed.KnownSeeds(),
	}, nil
}

func greetURL(endpoint string) (*url.URL, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("%w: empty endpoint", ErrGreetFailed)
	}

	return &url.URL{
		Scheme: "http",
		Host:   endpoint,
		Path:   yacyproto.PathHello,
	}, nil
}
