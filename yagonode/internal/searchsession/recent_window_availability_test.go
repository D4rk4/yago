package searchsession

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type heldSessionExtension struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (s *heldSessionExtension) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	call := s.calls.Add(1)
	if call == 2 {
		close(s.started)
		<-s.release
	}
	results := make([]searchcore.Result, req.Limit)
	for index := range results {
		results[index] = searchcore.Result{
			Title: fmt.Sprintf("result-%d", index),
			URL:   fmt.Sprintf("https://example.test/%d", index),
		}
	}

	return searchcore.Response{TotalResults: 100, Results: results}, nil
}

func TestRecentWindowRemainsAvailableDuringExtension(t *testing.T) {
	inner := &heldSessionExtension{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(inner.release) }) })
	stable := NewStableWindow(inner)
	request := searchcore.Request{Query: "stable", Limit: 10}
	if _, err := stable.Search(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	extension := make(chan error, 1)
	go func() {
		_, err := stable.Search(t.Context(), searchcore.Request{
			Query: "stable", Offset: 50, Limit: 10,
		})
		extension <- err
	}()
	<-inner.started
	recent := make(chan searchcore.Response, 1)
	go func() {
		response, found := stable.Recent(request)
		if !found {
			response.Results = nil
		}
		recent <- response
	}()
	select {
	case response := <-recent:
		if len(response.Results) != 10 || response.Results[0].Title != "result-0" {
			t.Fatalf("recent response = %#v", response)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("recent response waited for the active extension")
	}
	release.Do(func() { close(inner.release) })
	if err := <-extension; err != nil {
		t.Fatal(err)
	}
}

func TestRecentSuccessUsesCacheForOperationalErrorOnly(t *testing.T) {
	stable := NewStableWindow(&refreshSequenceSearcher{responses: []searchcore.Response{{
		Results: []searchcore.Result{{URL: "https://cached.example/"}},
	}}})
	request := searchcore.Request{Query: "cached", Limit: 10}
	if _, err := stable.Search(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	want := errors.New("storage unavailable")
	response, err := WithRecentSuccessOnIncompleteRefresh(
		&refreshSequenceSearcher{errors: []error{want}},
		stable,
	).Search(t.Context(), request)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://cached.example/" {
		t.Fatalf("operational fallback = %#v, %v", response, err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	response, err = WithRecentSuccessOnIncompleteRefresh(
		&refreshSequenceSearcher{errors: []error{want}},
		stable,
	).Search(canceled, request)
	if !errors.Is(err, want) || len(response.Results) != 0 {
		t.Fatalf("canceled fallback = %#v, %v", response, err)
	}
}
