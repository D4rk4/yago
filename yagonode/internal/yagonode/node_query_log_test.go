package yagonode

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestParseQueryLogMode(t *testing.T) {
	t.Parallel()

	cases := map[string]queryLogMode{
		"":          queryLogOff,
		"off":       queryLogOff,
		"aggregate": queryLogAggregate,
		"full":      queryLogFull,
	}
	for raw, want := range cases {
		got, err := parseQueryLogMode(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got != want {
			t.Errorf("parse %q = %q, want %q", raw, got, want)
		}
	}

	if _, err := parseQueryLogMode("verbose"); err == nil {
		t.Error("expected an error for an unknown mode")
	}
}

func TestWithQueryLoggingOffDoesNotWrap(t *testing.T) {
	t.Parallel()

	for _, mode := range []queryLogMode{queryLogOff, ""} {
		if _, ok := withQueryLogging(stubPrimarySearcher{}, mode, nil).(queryLoggingSearcher); ok {
			t.Errorf("mode %q must not wrap the searcher", mode)
		}
	}
}

func TestWithQueryLoggingWrapsActiveModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []queryLogMode{queryLogAggregate, queryLogFull} {
		if _, ok := withQueryLogging(stubPrimarySearcher{}, mode, nil).(queryLoggingSearcher); !ok {
			t.Errorf("mode %q must wrap the searcher", mode)
		}
	}
}

func loggedSearch(t *testing.T, mode queryLogMode, query string, total int) string {
	t.Helper()

	var buf bytes.Buffer
	searcher := queryLoggingSearcher{
		next:   stubPrimarySearcher{resp: searchcore.Response{TotalResults: total}},
		mode:   mode,
		logger: slog.New(slog.NewJSONHandler(&buf, nil)),
	}
	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: query},
	); err != nil {
		t.Fatalf("search: %v", err)
	}

	return buf.String()
}

func TestQueryLoggingAggregateOmitsQueryText(t *testing.T) {
	t.Parallel()

	out := loggedSearch(t, queryLogAggregate, "private banking password", 4)
	if strings.Contains(out, "private banking password") {
		t.Fatalf("aggregate mode leaked the query text: %s", out)
	}
	if !strings.Contains(out, "queryLength") || !strings.Contains(out, `"results":4`) {
		t.Fatalf("aggregate mode dropped its metrics: %s", out)
	}
}

func TestQueryLoggingFullRecordsQueryText(t *testing.T) {
	t.Parallel()

	out := loggedSearch(t, queryLogFull, "public news query", 1)
	if !strings.Contains(out, "public news query") {
		t.Fatalf("full mode dropped the query text: %s", out)
	}
}
