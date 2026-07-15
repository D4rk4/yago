package yagonode

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchactivity"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type failedQueryLogSearcher struct{}

func (failedQueryLogSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return searchcore.Response{}, errors.New("search failed")
}

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

func TestQueryActivityDistinguishesErrorsAndPartialAnswersFromMisses(t *testing.T) {
	tracker := searchactivity.New(searchactivity.ModeAggregate)
	logger := slog.New(slog.DiscardHandler)
	errored := queryLoggingSearcher{
		next: failedQueryLogSearcher{}, mode: queryLogAggregate, logger: logger, tracker: tracker,
	}
	if _, err := errored.Search(t.Context(), searchcore.Request{Query: "error"}); err == nil {
		t.Fatal("errored search must return its error")
	}
	partial := queryLoggingSearcher{
		next: stubPrimarySearcher{resp: searchcore.Response{
			PartialFailures: []searchcore.PartialFailure{{Source: "remote-stage"}},
		}},
		mode: queryLogAggregate, logger: logger, tracker: tracker,
	}
	if _, err := partial.Search(t.Context(), searchcore.Request{Query: "partial"}); err != nil {
		t.Fatalf("partial search: %v", err)
	}

	entries, total, confirmedZero := tracker.Snapshot()
	if len(entries) != 2 || total != 2 || confirmedZero != 0 {
		t.Fatalf(
			"activity = entries %d, total %d, confirmed zero %d",
			len(entries),
			total,
			confirmedZero,
		)
	}
	if entries[0].Failed || !entries[0].Incomplete ||
		!entries[1].Failed || entries[1].Incomplete {
		t.Fatalf("activity states = %+v", entries)
	}
}
