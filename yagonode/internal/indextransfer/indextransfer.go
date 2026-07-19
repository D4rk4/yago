// Package indextransfer owns outbound YaCy index transfer HTTP calls.
package indextransfer

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

const transferMaxBodyBytes int64 = 256 << 10

var (
	errTransferFailed    = errors.New("index transfer failed")
	errTransferTransport = errors.New("peer transport failed")
)

type HTTPPeerWriter struct {
	client      *http.Client
	networkName string
	self        yagomodel.Seed
	preferHTTPS bool
	access      yagoproto.NetworkAccess
	signForm    func(url.Values) error
}

var (
	newTransferRequest   = http.NewRequestWithContext
	parseTransferMessage = yagomodel.ParseMessage
)

type transferPost[T any] struct {
	ctx         context.Context
	client      *http.Client
	peer        yagomodel.Seed
	path        string
	form        url.Values
	parse       func(yagomodel.Message) (T, error)
	preferHTTPS bool
}

func NewHTTPPeerWriter(
	client *http.Client,
	networkName string,
	self yagomodel.Seed,
	preferHTTPS bool,
	access ...yagoproto.NetworkAccess,
) HTTPPeerWriter {
	if client == nil {
		client = http.DefaultClient
	}

	configured := yagoproto.NetworkAccess{NetworkName: networkName, Self: self.Hash}
	if len(access) != 0 {
		configured = access[0]
		configured.Self = self.Hash
	}

	return HTTPPeerWriter{
		client:      client,
		networkName: networkName,
		self:        self,
		preferHTTPS: preferHTTPS,
		access:      configured,
		signForm:    configured.Sign,
	}
}

func (w HTTPPeerWriter) TransferRWI(
	ctx context.Context,
	peer yagomodel.Seed,
	postings []yagomodel.RWIPosting,
) (yagoproto.TransferRWIResponse, error) {
	if len(postings) == 0 {
		return yagoproto.TransferRWIResponse{Result: yagoproto.ResultOK}, nil
	}

	req := yagoproto.TransferRWIRequest{
		NetworkName: w.networkName,
		Iam:         w.self.Hash,
		YouAre:      peer.Hash,
		WordCount:   wordCount(postings),
		EntryCount:  len(postings),
		Indexes:     postings,
	}

	form := req.Form()
	if err := w.sign(form); err != nil {
		return yagoproto.TransferRWIResponse{}, err
	}

	return postTransfer(
		transferPost[yagoproto.TransferRWIResponse]{
			ctx:         ctx,
			client:      w.client,
			peer:        peer,
			path:        yagoproto.PathTransferRWI,
			form:        form,
			parse:       yagoproto.ParseTransferRWIResponse,
			preferHTTPS: w.preferHTTPS,
		},
	)
}

func (w HTTPPeerWriter) TransferURL(
	ctx context.Context,
	peer yagomodel.Seed,
	rows []yagomodel.URIMetadataRow,
) (yagoproto.TransferURLResponse, error) {
	req := yagoproto.TransferURLRequest{
		NetworkName: w.networkName,
		Iam:         w.self.Hash,
		YouAre:      peer.Hash,
		URLCount:    len(rows),
		URLs:        rows,
	}

	form := req.Form()
	if err := w.sign(form); err != nil {
		return yagoproto.TransferURLResponse{}, err
	}

	return postTransfer(
		transferPost[yagoproto.TransferURLResponse]{
			ctx:         ctx,
			client:      w.client,
			peer:        peer,
			path:        yagoproto.PathTransferURL,
			form:        form,
			parse:       yagoproto.ParseTransferURLResponse,
			preferHTTPS: w.preferHTTPS,
		},
	)
}

func (w HTTPPeerWriter) sign(form url.Values) error {
	if w.access.Mode != yagoproto.NetworkAuthenticationSaltedMagic {
		return nil
	}
	if err := w.signForm(form); err != nil {
		return fmt.Errorf("%w: %w", errTransferFailed, err)
	}

	return nil
}

func postTransfer[T any](post transferPost[T]) (T, error) {
	var zero T

	targets, err := post.peer.ProtocolEndpoints(post.path, post.preferHTTPS)
	if err != nil {
		return zero, fmt.Errorf("%w: target: %w", errTransferFailed, err)
	}

	var lastErr error
	for _, target := range targets {
		parsed, err := postTransferTo(post, target)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
		// Retry over the next candidate scheme only when the transport
		// failed; an HTTP status means the peer answered (YaCy retries
		// https as http on IOException only).
		if !errors.Is(err, errTransferTransport) {
			break
		}
	}

	return zero, lastErr
}

func postTransferTo[T any](post transferPost[T], target *url.URL) (T, error) {
	var zero T

	req, err := newTransferRequest(
		post.ctx,
		http.MethodPost,
		target.String(),
		strings.NewReader(post.form.Encode()),
	)
	if err != nil {
		return zero, fmt.Errorf("%w: request: %w", errTransferFailed, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := post.client.Do(req)
	if err != nil {
		return zero, fmt.Errorf("%w: post: %w: %w", errTransferFailed, errTransferTransport, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("%w: status %d", errTransferFailed, resp.StatusCode)
	}

	msg, err := readTransferMessage(resp.Body)
	if err != nil {
		return zero, err
	}

	parsed, err := post.parse(msg)
	if err != nil {
		return zero, fmt.Errorf("%w: response: %w", errTransferFailed, err)
	}

	return parsed, nil
}

func readTransferMessage(body io.Reader) (yagomodel.Message, error) {
	raw, err := io.ReadAll(io.LimitReader(body, transferMaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %w", errTransferFailed, err)
	}
	if int64(len(raw)) > transferMaxBodyBytes {
		return nil, fmt.Errorf("%w: response too large", errTransferFailed)
	}

	msg, err := parseTransferMessage(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: parse response: %w", errTransferFailed, err)
	}

	return msg, nil
}

func wordCount(postings []yagomodel.RWIPosting) int {
	seen := make(map[yagomodel.Hash]struct{}, len(postings))
	for _, posting := range postings {
		seen[posting.WordHash] = struct{}{}
	}

	return len(seen)
}
