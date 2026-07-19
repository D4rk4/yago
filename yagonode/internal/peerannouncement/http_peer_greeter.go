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

var (
	errGreetFailed    = errors.New("peer greet failed")
	errGreetTransport = errors.New("peer transport failed")
)

type greetResult struct {
	YourIP   string
	YourType yagomodel.PeerType
	Known    []yagomodel.Seed
}

type httpPeerGreeter struct {
	client      *http.Client
	networkName string
	preferHTTPS bool
	access      yagoproto.NetworkAccess
	signForm    func(yagoproto.NetworkAccess, url.Values) error
}

var (
	newGreetRequest   = http.NewRequestWithContext
	parseGreetMessage = yagomodel.ParseMessage
)

func newHTTPPeerGreeter(
	client *http.Client,
	networkName string,
	preferHTTPS bool,
	access ...yagoproto.NetworkAccess,
) httpPeerGreeter {
	configured := yagoproto.NetworkAccess{NetworkName: networkName}
	if len(access) != 0 {
		configured = access[0]
	}

	return httpPeerGreeter{
		client: client, networkName: networkName, preferHTTPS: preferHTTPS, access: configured,
		signForm: yagoproto.NetworkAccess.Sign,
	}
}

func (g httpPeerGreeter) Greet(
	ctx context.Context,
	target yagomodel.Seed,
	self yagomodel.Seed,
	count int,
) (greetResult, error) {
	endpoints, err := greetEndpoints(target, g.preferHTTPS)
	if err != nil {
		return greetResult{}, err
	}

	request := yagoproto.HelloRequest{
		NetworkName: g.networkName,
		Seed:        self,
		Count:       count,
		Iam:         self.Hash,
	}
	formValues := request.Form()
	if g.access.Mode == yagoproto.NetworkAuthenticationSaltedMagic {
		access := g.access
		access.Self = self.Hash
		if err := g.signForm(access, formValues); err != nil {
			return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
		}
	}
	form := formValues.Encode()

	var lastErr error
	for _, endpoint := range endpoints {
		result, err := g.greetEndpoint(ctx, endpoint, form)
		if err == nil {
			return result, nil
		}
		lastErr = err
		// Retry over the next candidate scheme only when the transport
		// failed; an HTTP status means the peer answered (YaCy retries
		// https as http on IOException only).
		if !errors.Is(err, errGreetTransport) {
			break
		}
	}

	return greetResult{}, lastErr
}

func (g httpPeerGreeter) greetEndpoint(
	ctx context.Context,
	target *url.URL,
	form string,
) (greetResult, error) {
	req, err := newGreetRequest(ctx, http.MethodPost, target.String(), strings.NewReader(form))
	if err != nil {
		return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client.Do(req)
	if err != nil {
		return greetResult{}, fmt.Errorf("%w: %w: %w", errGreetFailed, errGreetTransport, err)
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

func greetEndpoints(target yagomodel.Seed, preferHTTPS bool) ([]*url.URL, error) {
	endpoints, err := target.ProtocolEndpoints(yagoproto.PathHello, preferHTTPS)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errGreetFailed, err)
	}

	return endpoints, nil
}
