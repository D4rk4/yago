package searchsession

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type pendingWindowSearcher struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (s *pendingWindowSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	if s.calls.Add(1) > 1 {
		close(s.started)
		<-s.release
	}
	results := make([]searchcore.Result, 51)
	for ordinal := range results {
		results[ordinal].URL = fmt.Sprintf("https://example.org/page/%d", ordinal)
	}

	return searchcore.Response{Results: results, TotalResults: 200}, nil
}

func TestStableWindowDoesNotBlockReadyPagesOrCanceledWaiters(t *testing.T) {
	for _, test := range []struct {
		offset int
		rows   int
		err    error
	}{
		{offset: 10, rows: 10},
		{offset: 70, err: context.Canceled},
	} {
		t.Run(fmt.Sprint(test.offset), func(t *testing.T) {
			inner := &pendingWindowSearcher{
				started: make(chan struct{}),
				release: make(chan struct{}),
			}
			stable := NewStableWindow(inner)
			request := searchcore.Request{Query: "synthetic", Limit: 10}
			if _, err := stable.Search(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			firstDone := make(chan struct{})
			go func() {
				defer close(firstDone)
				_, _ = stable.Search(
					t.Context(),
					searchcore.Request{Query: request.Query, Offset: 60, Limit: 10},
				)
			}()
			<-inner.started
			defer func() { close(inner.release); <-firstDone }()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request.Offset = test.offset
			if test.err != nil {
				cancel()
			}
			response, err := readPendingSessionPage(t, ctx, stable, request)
			if !errors.Is(err, test.err) || len(response.Results) != test.rows {
				t.Fatalf("rows=%d error=%v", len(response.Results), err)
			}
		})
	}
}

func readPendingSessionPage(
	t *testing.T,
	ctx context.Context,
	stable StableWindow,
	request searchcore.Request,
) (searchcore.Response, error) {
	t.Helper()
	type outcome struct {
		response searchcore.Response
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		response, err := stable.Search(ctx, request)
		done <- outcome{response: response, err: err}
	}()
	select {
	case result := <-done:
		return result.response, result.err
	case <-time.After(200 * time.Millisecond):
		t.Fatal("page request blocked behind an unrelated extension")
		return searchcore.Response{}, nil
	}
}

func TestSessionExtensionRefusesCanceledAdmissionWithoutRetainingSlot(t *testing.T) {
	entry := &session{extension: make(chan struct{}, 1)}
	for range 64 {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := entry.acquireExtension(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("admission error=%v", err)
		}
		if len(entry.extension) != 0 {
			t.Fatal("canceled admission retained the extension slot")
		}
	}
	if err := entry.acquireExtension(t.Context()); err != nil || len(entry.extension) != 1 {
		t.Fatalf("live admission error=%v slots=%d", err, len(entry.extension))
	}
	<-entry.extension
}
