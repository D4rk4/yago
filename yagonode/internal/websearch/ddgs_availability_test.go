package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

func TestParallelReportsUnavailableEnginesAsPartialFailure(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (
		*http.Response,
		error,
	) {
		calls.Add(1)

		return htmlResponse(http.StatusTooManyRequests, ""), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendDuckDuckGo, Now: fixedClock(),
	})
	primary := &stubSearcher{resp: searchcore.Response{
		Results:      []searchcore.Result{{Title: "Local", URL: "https://local.example/"}},
		TotalResults: 1,
	}}
	searcher := NewParallelSearcher(primary, provider, enabled)

	for range 2 {
		response, err := searcher.Search(context.Background(), searchcore.Request{
			Query: "query", Limit: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Results) != 1 || response.Results[0].Source == searchcore.SourceWeb {
			t.Fatalf("primary response = %#v", response.Results)
		}
		if len(response.PartialFailures) != 1 ||
			response.PartialFailures[0] != webProviderFailure(errWebSearchEnginesUnavailable) {
			t.Fatalf("partial failures = %#v", response.PartialFailures)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("engine calls = %d, want one attempt per DuckDuckGo endpoint", got)
	}
	if _, err := provider.Search(context.Background(), "query", 10); !errors.Is(
		err,
		errWebSearchEnginesUnavailable,
	) {
		t.Fatalf("backed-off provider error = %v", err)
	}
}

func TestUnavailableReportingTracksRecoveryAndIgnoresCancellation(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	now := time.Unix(1_700_000_000, 0)
	var status atomic.Int32
	status.Store(http.StatusTooManyRequests)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (
		*http.Response,
		error,
	) {
		if err := request.Context().Err(); err != nil {
			return nil, fmt.Errorf("request context: %w", err)
		}

		return htmlResponse(int(status.Load()), "answer"), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client,
		Now:    func() time.Time { return now },
	})
	provider.engines = []engine{tracingEngine(
		"available",
		Result{Title: "available", URL: "https://available.example/"},
	)}

	canceledContext, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := provider.Search(canceledContext, "canceled", 10); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("canceled search error = %v", err)
	}
	if strings.Contains(output.String(), msgWebSearchEnginesUnavailable) {
		t.Fatalf("canceled search logged an outage: %s", output.String())
	}

	for _, query := range []string{"first outage", "repeated outage"} {
		if _, err := provider.Search(t.Context(), query, 10); !errors.Is(
			err,
			errWebSearchEnginesUnavailable,
		) {
			t.Fatalf("search %q error = %v", query, err)
		}
	}
	now = now.Add(minBackoff)
	status.Store(http.StatusOK)
	if results, err := provider.Search(t.Context(), "recovered", 10); err != nil ||
		len(results) != 1 {
		t.Fatalf("recovery results = %#v error = %v", results, err)
	}
	status.Store(http.StatusTooManyRequests)
	if _, err := provider.Search(t.Context(), "second outage", 10); !errors.Is(
		err,
		errWebSearchEnginesUnavailable,
	) {
		t.Fatalf("second outage error = %v", err)
	}
	if got := strings.Count(output.String(), msgWebSearchEnginesUnavailable); got != 2 {
		t.Fatalf("outage warnings = %d, want 2: %s", got, output.String())
	}
}

func TestUnavailableLoggingDoesNotExposeSubmittedQuery(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	ctx, _ := tracectx.StartServerSpan(
		t.Context(),
		"00-11111111111111112222222222222222-3333333333333333-01",
	)
	serverSpanID, ok := tracectx.ServerSpanIDFromContext(ctx)
	if !ok {
		t.Fatal("server span ID missing")
	}

	provider := NewDDGSProvider(DDGSConfig{
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (
			*http.Response,
			error,
		) {
			return nil, errors.New("PRIVATE_ERROR_CANARY https://PRIVATE_URL_CANARY/")
		})},
		Backend: backendDuckDuckGo,
		Now:     fixedClock(),
	})
	primary := &stubSearcher{resp: searchcore.Response{
		Results:      []searchcore.Result{{Title: "Local", URL: "https://local.example/"}},
		TotalResults: 1,
	}}
	searcher := NewParallelSearcher(primary, provider, enabled)
	response, err := searcher.Search(ctx, searchcore.Request{
		Query: "PRIVATE_QUERY_CANARY", Limit: 10,
	})
	if err != nil || len(response.Results) != 1 {
		t.Fatalf("response = %#v error = %v", response, err)
	}
	logged := output.String()
	if !strings.Contains(logged, msgWebSearchEnginesUnavailable) ||
		!strings.Contains(logged, msgFallbackFailed) {
		t.Fatalf("missing outage logs: %s", logged)
	}
	assertWebSearchLogOmits(t, logged,
		"PRIVATE_QUERY_CANARY",
		"PRIVATE_ERROR_CANARY",
		"PRIVATE_URL_CANARY",
		"11111111111111112222222222222222",
		"3333333333333333",
		"?q=",
	)
	assertWebSearchLogMessagesUseServerSpan(t, logged, serverSpanID,
		msgWebSearchEnginesUnavailable,
		msgFallbackFailed,
		msgWebSearchEngineAttempt,
	)
}

func assertWebSearchLogMessagesUseServerSpan(
	t *testing.T,
	logged, serverSpanID string,
	messages ...string,
) {
	t.Helper()
	for _, message := range messages {
		found := false
		for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
			var record map[string]any
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatal(err)
			}
			if record["msg"] == message {
				found = true
				if record["serverSpanId"] != serverSpanID {
					t.Fatalf(
						"%s span = %#v, want %q",
						message,
						record["serverSpanId"],
						serverSpanID,
					)
				}
			}
		}
		if !found {
			t.Fatalf("missing %s: %s", message, logged)
		}
	}
}

func assertWebSearchLogOmits(t *testing.T, logged string, canaries ...string) {
	t.Helper()
	for _, secret := range canaries {
		if strings.Contains(logged, secret) {
			t.Fatalf("web search log exposed %q: %s", secret, logged)
		}
	}
}

func TestWebSearchFailureReason(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: nil, want: webSearchFailureNone},
		{err: errWebSearchEnginesUnavailable, want: webSearchFailureUnavailable},
		{err: context.Canceled, want: webSearchFailureCanceled},
		{err: context.DeadlineExceeded, want: webSearchFailureDeadline},
	}
	for _, test := range tests {
		if got := webSearchFailureReason(test.err); got != test.want {
			t.Errorf("failure reason = %q, want %q", got, test.want)
		}
	}
}

func TestWebSearchFailureCauseUsesTypedCategories(t *testing.T) {
	for _, test := range []struct {
		err  error
		want searchcore.FailureCause
	}{
		{nil, searchcore.FailureCauseUnknown},
		{context.DeadlineExceeded, searchcore.FailureCauseDeadline},
		{context.Canceled, searchcore.FailureCauseCanceled},
		{errWebSearchEnginesUnavailable, searchcore.FailureCauseUnavailable},
		{errStrictSafeSearchUnavailable, searchcore.FailureCauseUnsupported},
		{errors.New("provider failed"), searchcore.FailureCauseBackend},
	} {
		if got := webSearchFailureCause(test.err); got != test.want {
			t.Errorf("web search failure cause for %v = %d, want %d", test.err, got, test.want)
		}
	}
}

// TestProviderFailureIsVisibleAtProductionLogLevel pins the level, not the
// text. Both provider-failure records were written at Debug, and a node runs at
// Info, so on a deployment where the provider failed on half of all searches
// the only trace was an aggregate outage warning with a bucketed reason: the
// per-engine record that names the engine and says whether it was rate limited
// never reached the journal at all. A handler admitting Info and above is the
// whole point of the test -- under the old level it captures nothing.
func TestProviderFailureIsVisibleAtProductionLogLevel(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	provider := NewDDGSProvider(DDGSConfig{
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (
			*http.Response,
			error,
		) {
			return nil, errors.New("dial refused")
		})},
		Backend: backendDuckDuckGo,
		Now:     fixedClock(),
	})
	primary := &stubSearcher{resp: searchcore.Response{
		Results:      []searchcore.Result{{Title: "Local", URL: "https://local.example/"}},
		TotalResults: 1,
	}}
	if _, err := NewParallelSearcher(primary, provider, enabled).Search(
		t.Context(),
		searchcore.Request{Query: "private-search-phrase", Limit: 10},
	); err != nil {
		t.Fatalf("search: %v", err)
	}

	logged := output.String()
	for _, want := range []string{
		msgFallbackFailed,
		msgWebSearchEngineAttempt,
		// The engine and its rate-limit state are what distinguish a blocked
		// scrape from a throttled one; a bucketed reason alone cannot.
		`"engine":"`,
		`"rateLimited":`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("production log level lost %q: %s", want, logged)
		}
	}
	// Raising the level must not raise the query with it.
	for _, secret := range []string{"private-search-phrase", "private+search+phrase", "?q="} {
		if strings.Contains(logged, secret) {
			t.Fatalf("visible log exposed %q: %s", secret, logged)
		}
	}
}
