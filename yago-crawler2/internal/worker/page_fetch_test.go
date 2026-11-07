package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/httpfetch"
)

func TestFetchRobotsPageBoundsOnlyItsNetworkRequest(t *testing.T) {
	started := make(chan struct{})
	var requestDeadline time.Time
	fetcher := pageFetcherFunc(func(ctx context.Context, _ string) (httpfetch.PageResponse, error) {
		requestDeadline, _ = ctx.Deadline()
		close(started)
		<-ctx.Done()

		return httpfetch.PageResponse{}, ctx.Err()
	})
	worker := &Worker{fetcher: fetcher, hosts: newHostAdmission(0, 1)}
	startedAt := time.Now()
	_, err := worker.fetchRobotsPage(context.Background(), "https://example.org/robots.txt")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("robots request error = %v, want its bounded deadline", err)
	}
	if elapsed := time.Since(startedAt); elapsed > robotsFetchTimeout+time.Second {
		t.Fatalf("robots request elapsed %s, over its bounded network time", elapsed)
	}
	select {
	case <-started:
	default:
		t.Fatal("robots fetch did not reach the test server")
	}
	if requestWindow := requestDeadline.Sub(
		startedAt,
	); requestWindow > robotsFetchTimeout+time.Second ||
		requestWindow < robotsFetchTimeout-time.Second {
		t.Fatalf("robots request window = %s", requestWindow)
	}
}

func TestFetchPageKeepsCallerRequestDeadline(t *testing.T) {
	fetcher := pageFetcherFunc(func(ctx context.Context, _ string) (httpfetch.PageResponse, error) {
		if _, hasDeadline := ctx.Deadline(); hasDeadline {
			return httpfetch.PageResponse{}, errors.New("unexpected page request deadline")
		}

		return httpfetch.PageResponse{}, nil
	})
	worker := &Worker{fetcher: fetcher, hosts: newHostAdmission(0, 1)}
	if _, err := worker.fetchPage(context.Background(), "https://example.org/page"); err != nil {
		t.Fatalf("page request: %v", err)
	}
}

type pageFetcherFunc func(context.Context, string) (httpfetch.PageResponse, error)

func (fetcher pageFetcherFunc) Fetch(
	ctx context.Context,
	pageURL string,
) (httpfetch.PageResponse, error) {
	return fetcher(ctx, pageURL)
}

func (pageFetcherFunc) SetMaximumRedirects(int) bool {
	return true
}

func (pageFetcherFunc) CloseIdleConnections() {}
