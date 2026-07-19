package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

type blockingFetchStartAdmission struct {
	started chan struct{}
	release chan struct{}
}

type fetchStartPageSource func(
	context.Context,
	*url.URL,
) (pagefetch.FetchedPage, error)

func (source fetchStartPageSource) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	return source(ctx, target)
}

func (admission blockingFetchStartAdmission) Wait(ctx context.Context) error {
	close(admission.started)
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for fetch-start admission: %w", ctx.Err())
	case <-admission.release:
		return nil
	}
}

func TestFetchStartAdmissionGatesFetcher(t *testing.T) {
	admission := blockingFetchStartAdmission{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	fetched := make(chan struct{})
	pipeline := NewPipeline(nil, fetchStartPageSource(func(
		context.Context,
		*url.URL,
	) (pagefetch.FetchedPage, error) {
		close(fetched)

		return pagefetch.FetchedPage{}, nil
	}), nil, nil, WithFetchStartAdmission(admission))
	done := make(chan error, 1)
	go func() {
		_, err := pipeline.fetchJob(
			t.Context(),
			crawljob.CrawlJob{},
			&url.URL{Scheme: "https", Host: "example.org"},
			nil,
		)
		done <- err
	}()
	select {
	case <-admission.started:
	case <-time.After(time.Second):
		t.Fatal("fetch admission was not consulted")
	}
	select {
	case <-fetched:
		t.Fatal("fetcher ran before process admission")
	case <-time.After(20 * time.Millisecond):
	}
	close(admission.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("fetch after admission: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fetcher did not run after process admission")
	}
}

func TestFetchStartAdmissionStopsCancelledFetch(t *testing.T) {
	admission := blockingFetchStartAdmission{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	fetched := false
	pipeline := NewPipeline(nil, fetchStartPageSource(func(
		context.Context,
		*url.URL,
	) (pagefetch.FetchedPage, error) {
		fetched = true

		return pagefetch.FetchedPage{}, nil
	}), nil, nil, WithFetchStartAdmission(admission))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := pipeline.fetchJob(
			ctx,
			crawljob.CrawlJob{},
			&url.URL{Scheme: "https", Host: "example.org"},
			nil,
		)
		done <- err
	}()
	select {
	case <-admission.started:
	case <-time.After(time.Second):
		t.Fatal("fetch admission was not consulted")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission = %v, want context cancellation", err)
	}
	if fetched {
		t.Fatal("cancelled admission reached the fetcher")
	}
}
