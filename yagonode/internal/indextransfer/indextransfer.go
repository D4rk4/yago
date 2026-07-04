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

var errTransferFailed = errors.New("index transfer failed")

type HTTPPeerWriter struct {
	client      *http.Client
	networkName string
	self        yagomodel.Seed
}

var (
	newTransferRequest   = http.NewRequestWithContext
	parseTransferMessage = yagomodel.ParseMessage
)

type transferPost[T any] struct {
	ctx    context.Context
	client *http.Client
	peer   yagomodel.Seed
	path   string
	form   url.Values
	parse  func(yagomodel.Message) (T, error)
}

func NewHTTPPeerWriter(
	client *http.Client,
	networkName string,
	self yagomodel.Seed,
) HTTPPeerWriter {
	if client == nil {
		client = http.DefaultClient
	}

	return HTTPPeerWriter{client: client, networkName: networkName, self: self}
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

	return postTransfer(
		transferPost[yagoproto.TransferRWIResponse]{
			ctx:    ctx,
			client: w.client,
			peer:   peer,
			path:   yagoproto.PathTransferRWI,
			form:   req.Form(),
			parse:  yagoproto.ParseTransferRWIResponse,
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

	return postTransfer(
		transferPost[yagoproto.TransferURLResponse]{
			ctx:    ctx,
			client: w.client,
			peer:   peer,
			path:   yagoproto.PathTransferURL,
			form:   req.Form(),
			parse:  yagoproto.ParseTransferURLResponse,
		},
	)
}

func postTransfer[T any](post transferPost[T]) (T, error) {
	var zero T

	target, err := post.peer.HTTPEndpoint(post.path)
	if err != nil {
		return zero, fmt.Errorf("%w: target: %w", errTransferFailed, err)
	}

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
		return zero, fmt.Errorf("%w: post: %w", errTransferFailed, err)
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
