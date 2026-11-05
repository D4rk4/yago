package peerannouncement

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagoproto"
)

const greetMaxBodyBytes int64 = 256 << 10

var (
	errGreetFailed    = errors.New("peer greet failed")
	errGreetTransport = errors.New("peer transport failed")
)

type greetResult struct {
	Responder         yagomodel.Seed
	ContactedHost     yagomodel.Optional[yagomodel.Host]
	ContactedEndpoint peerwire.Endpoint
	YourIP            string
	YourType          yagomodel.PeerType
	Known             []yagomodel.Seed
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
	answered yagomodel.Optional[peerwire.Endpoint],
) (greetResult, error) {
	endpoints, err := greetEndpoints(target, g.preferHTTPS, answered)
	if err != nil {
		return greetResult{}, err
	}

	request := yagoproto.HelloRequest{
		NetworkName: g.networkName,
		Seed:        self,
		Count:       count,
		Iam:         self.Hash.String(),
	}
	formValues := request.Form()
	if g.access.Mode == yagoproto.NetworkAuthenticationSaltedMagic {
		access := g.access
		access.Self = self.Hash
		if err := g.signForm(access, formValues); err != nil {
			return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
		}
	}
	operationContext, cancelOperation := g.operationContext(ctx)
	defer cancelOperation()

	body, contentType := peerwire.EncodeMultipartForm(formValues)
	var lastErr error
	for index, endpoint := range endpoints {
		if err := operationContext.Err(); err != nil {
			return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
		}
		attemptContext, cancelAttempt := greetAttemptContext(
			operationContext,
			len(endpoints)-index,
		)
		result, err := g.greetEndpoint(attemptContext, endpoint, body, contentType, self)
		cancelAttempt()
		if err == nil {
			if result.Responder.Hash != target.Hash {
				lastErr = fmt.Errorf(
					"%w: responder hash %s does not match target %s",
					errGreetFailed,
					result.Responder.Hash,
					target.Hash,
				)
				continue
			}
			contactedHost, _ := yagomodel.ParseHost(endpoint.Hostname())
			result.ContactedHost = yagomodel.Some(contactedHost)
			result.ContactedEndpoint = peerwire.EndpointAt(endpoint)
			return result, nil
		}
		lastErr = err
		if operationContext.Err() != nil {
			return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, operationContext.Err())
		}
	}

	return greetResult{}, lastErr
}

func (g httpPeerGreeter) greetEndpoint(
	ctx context.Context,
	target *url.URL,
	body []byte,
	contentType string,
	self yagomodel.Seed,
) (greetResult, error) {
	req, err := newGreetRequest(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return greetResult{}, fmt.Errorf("%w: %w", errGreetFailed, err)
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(len(body))

	resp, err := g.client.Do(req)
	if err != nil {
		return greetResult{}, fmt.Errorf("%w: %w: %w", errGreetFailed, errGreetTransport, err)
	}
	defer closeResponseBody(ctx, resp.Body, "peerGreet")

	if resp.StatusCode != http.StatusOK {
		return greetResult{}, fmt.Errorf("%w: status %d", errGreetFailed, resp.StatusCode)
	}

	return parseGreetResponse(ctx, io.LimitReader(resp.Body, greetMaxBodyBytes), self)
}

func parseGreetResponse(
	ctx context.Context,
	body io.Reader,
	self yagomodel.Seed,
) (greetResult, error) {
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
	if !validGreetReportedAddresses(parsed.YourIP, self) {
		return greetResult{}, fmt.Errorf("%w: invalid reported caller address", errGreetFailed)
	}
	if parsed.YourType != yagomodel.PeerJunior &&
		parsed.YourType != yagomodel.PeerSenior &&
		parsed.YourType != yagomodel.PeerPrincipal {
		return greetResult{}, fmt.Errorf("%w: invalid caller peer type", errGreetFailed)
	}
	responder, ok := parsed.OwnSeed().Get()
	if !ok {
		return greetResult{}, fmt.Errorf("%w: missing responder seed", errGreetFailed)
	}

	return greetResult{
		Responder: responder,
		YourIP:    parsed.YourIP,
		YourType:  parsed.YourType,
		Known:     parsed.KnownSeeds(),
	}, nil
}

func greetEndpoints(
	target yagomodel.Seed,
	preferHTTPS bool,
	answered yagomodel.Optional[peerwire.Endpoint],
) ([]*url.URL, error) {
	endpoints, err := peerwire.ContactEndpoints(target, yagoproto.PathHello, preferHTTPS, answered)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errGreetFailed, err)
	}

	return endpoints, nil
}
