package bootstrap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	seedlistMaxBodyBytes     int64 = 8 << 20
	seedlistMaxLineBytes           = 1 << 20
	seedlistMaxEntries             = 4096
	seedlistMaxRetainedBytes       = 16 << 20
)

var errSeedlistFetchFailed = errors.New("seedlist fetch failed")

type httpSeedlistFetcher struct {
	client *http.Client
}

func newHTTPSeedlistFetcher(client *http.Client) httpSeedlistFetcher {
	return httpSeedlistFetcher{client: client}
}

func (f httpSeedlistFetcher) Fetch(ctx context.Context, url string) ([]yagomodel.Seed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSeedlistFetchFailed, err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSeedlistFetchFailed, err)
	}
	defer closeResponseBody(ctx, resp.Body, "seedlistFetch")

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", errSeedlistFetchFailed, resp.StatusCode)
	}

	return decodeSeedlist(ctx, io.LimitReader(resp.Body, seedlistMaxBodyBytes), url)
}

func decodeSeedlist(ctx context.Context, body io.Reader, url string) ([]yagomodel.Seed, error) {
	var seeds []yagomodel.Seed
	entries := 0
	retainedBytes := 0
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), seedlistMaxLineBytes)
	for scanner.Scan() {
		entries++
		if entries > seedlistMaxEntries {
			break
		}
		line := scanner.Text()
		if line == "" {
			continue
		}

		seed, err := yagomodel.ParseSeedWireForm(ctx, line)
		if err != nil {
			slog.WarnContext(
				ctx,
				"seedlist line discarded",
				slog.String("url", url),
				slog.Any("error", err),
			)

			continue
		}

		seedBytes := seed.RetainedBytes()
		if seedBytes > seedlistMaxRetainedBytes-retainedBytes {
			continue
		}
		seeds = append(seeds, seed)
		retainedBytes += seedBytes
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", errSeedlistFetchFailed, err)
	}

	return seeds, nil
}
