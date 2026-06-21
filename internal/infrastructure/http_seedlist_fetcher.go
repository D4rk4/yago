package infrastructure

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	seedlistMaxBodyBytes int64 = 8 << 20
	seedlistMaxLineBytes       = 1 << 20
)

var ErrSeedlistFetchFailed = errors.New("seedlist fetch failed")

type HTTPSeedlistFetcher struct {
	client *http.Client
}

func NewHTTPSeedlistFetcher(client *http.Client) *HTTPSeedlistFetcher {
	return &HTTPSeedlistFetcher{client: client}
}

func (f *HTTPSeedlistFetcher) Fetch(ctx context.Context, url string) ([]yacymodel.Seed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSeedlistFetchFailed, err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSeedlistFetchFailed, err)
	}
	defer closeResponseBody(ctx, resp.Body, "seedlistFetch")

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrSeedlistFetchFailed, resp.StatusCode)
	}

	return decodeSeedlist(ctx, io.LimitReader(resp.Body, seedlistMaxBodyBytes), url)
}

func decodeSeedlist(ctx context.Context, body io.Reader, url string) ([]yacymodel.Seed, error) {
	var seeds []yacymodel.Seed
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), seedlistMaxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		plain, err := yacymodel.DecodeWireForm(ctx, line)
		if err != nil {
			slog.WarnContext(
				ctx,
				"seedlist line discarded",
				slog.String("url", url),
				slog.Any("error", err),
			)

			continue
		}
		seed, err := yacymodel.ParseSeed(ctx, plain)
		if err != nil {
			slog.WarnContext(
				ctx,
				"seedlist line discarded",
				slog.String("url", url),
				slog.Any("error", err),
			)

			continue
		}

		seeds = append(seeds, seed)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSeedlistFetchFailed, err)
	}

	return seeds, nil
}

var _ ports.SeedlistFetcher = (*HTTPSeedlistFetcher)(nil)
