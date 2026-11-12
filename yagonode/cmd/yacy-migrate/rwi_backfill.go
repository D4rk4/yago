package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type rwiBackfillOptions struct {
	Endpoint         string
	APIKey           string
	Action           string
	MaximumDocuments int64
	DryRun           bool
}

type rwiBackfillStatus struct {
	State               string    `json:"state"`
	ScannedDocuments    int64     `json:"scannedDocuments"`
	IndexedDocuments    int64     `json:"indexedDocuments"`
	SkippedDocuments    int64     `json:"skippedDocuments"`
	PostingRows         int64     `json:"postingRows"`
	EstimatedBytes      int64     `json:"estimatedBytes"`
	MaximumDocumentsRun int64     `json:"maximumDocumentsRun"`
	DryRun              bool      `json:"dryRun"`
	LastError           string    `json:"lastError,omitempty"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type rwiBackfillRequest struct {
	Version          int    `json:"version"`
	Action           string `json:"action"`
	MaximumDocuments int64  `json:"maximumDocuments,omitempty"`
	DryRun           bool   `json:"dryRun,omitempty"`
}

func runRWIBackfillCommand(ctx context.Context, args []string, progress io.Writer) error {
	flags := flag.NewFlagSet("rwi-backfill", flag.ContinueOnError)
	flags.SetOutput(progress)
	opts := rwiBackfillOptions{}
	flags.StringVar(&opts.Endpoint, "endpoint", "", "Yago /admin/index/rwi-backfill URL")
	flags.StringVar(&opts.Action, "action", "status", "status, start, pause, or resume")
	flags.Int64Var(
		&opts.MaximumDocuments,
		"maximum-documents",
		0,
		"maximum documents this run; 0 means unlimited",
	)
	flags.BoolVar(
		&opts.DryRun,
		"dry-run",
		false,
		"measure artifact generation without writing RWI or URL metadata",
	)
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse rwi backfill options: %w", err)
	}
	opts.APIKey = strings.TrimSpace(os.Getenv("YAGO_API_KEY"))
	endpoint, err := validateRWIBackfillOptions(opts)
	if err != nil {
		return err
	}
	status, err := callRWIBackfill(ctx, &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, endpoint, opts)
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(status)
	if _, err := fmt.Fprintf(progress, "%s\n", encoded); err != nil {
		return fmt.Errorf("write rwi backfill status: %w", err)
	}
	return nil
}

func validateRWIBackfillOptions(opts rwiBackfillOptions) (*url.URL, error) {
	endpoint, err := url.Parse(opts.Endpoint)
	if err != nil || endpoint.Host == "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		endpoint.Path != "/admin/index/rwi-backfill" {
		return nil, errors.New(
			"-endpoint must be an HTTP(S) /admin/index/rwi-backfill URL without credentials or query",
		)
	}
	if endpoint.Scheme == "http" && !loopbackEndpoint(endpoint.Hostname()) {
		return nil, errors.New("-endpoint must use HTTPS unless it targets loopback")
	}
	switch opts.Action {
	case "status", "pause":
		if opts.MaximumDocuments != 0 || opts.DryRun {
			return nil, errors.New("status and pause do not accept run limits")
		}
	case "start", "resume":
		if opts.MaximumDocuments < 0 || opts.MaximumDocuments > 10_000_000 {
			return nil, errors.New("-maximum-documents must be between 0 and 10000000")
		}
	default:
		return nil, errors.New("-action must be status, start, pause, or resume")
	}
	if opts.APIKey == "" {
		return nil, errors.New("YAGO_API_KEY is required for rwi backfill control")
	}
	return endpoint, nil
}

func loopbackEndpoint(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func callRWIBackfill(
	ctx context.Context,
	client *http.Client,
	endpoint *url.URL,
	opts rwiBackfillOptions,
) (result rwiBackfillStatus, resultErr error) {
	method := http.MethodGet
	var body io.Reader
	if opts.Action != "status" {
		method = http.MethodPost
		encoded, _ := json.Marshal(rwiBackfillRequest{
			Version: 1, Action: opts.Action,
			MaximumDocuments: opts.MaximumDocuments, DryRun: opts.DryRun,
		})
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return rwiBackfillStatus{}, fmt.Errorf("create rwi backfill request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+opts.APIKey)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return rwiBackfillStatus{}, fmt.Errorf("send rwi backfill request: %w", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close rwi backfill response: %w", err))
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return rwiBackfillStatus{}, fmt.Errorf(
			"rwi backfill endpoint returned HTTP %d",
			response.StatusCode,
		)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return rwiBackfillStatus{}, fmt.Errorf("decode rwi backfill status: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return rwiBackfillStatus{}, errors.New("rwi backfill response contains trailing data")
	}
	return result, nil
}
