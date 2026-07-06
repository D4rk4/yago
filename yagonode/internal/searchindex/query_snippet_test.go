package searchindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestQueryBiasedSnippetCentersOnTerms(t *testing.T) {
	long := strings.Repeat("filler prologue words here. ", 40) +
		"The needle term appears deep inside the document body with context. " +
		strings.Repeat("trailing words after the match. ", 40)
	got := queryBiasedSnippet(long, []string{"needle"}, "fallback")
	if !strings.Contains(got, "needle term appears deep inside") {
		t.Fatalf("snippet missed the term window: %q", got)
	}
	if !strings.HasPrefix(got, "… ") {
		t.Fatalf("mid-text excerpt must carry an ellipsis: %q", got)
	}
	if len([]rune(got)) > snippetRuneCap+2 {
		t.Fatalf("snippet over cap: %d", len([]rune(got)))
	}
}

func TestQueryBiasedSnippetFallbacks(t *testing.T) {
	// Term missing falls back to the leading snippet.
	long := strings.Repeat("plain words without the query term at all. ", 40)
	got := queryBiasedSnippet(long, []string{"absent-zzz"}, "fallback")
	if !strings.HasPrefix(got, "plain words") {
		t.Fatalf("leading fallback = %q", got)
	}
	// Short texts return whole.
	if got := queryBiasedSnippet(
		"short text with needle",
		[]string{"needle"},
		"f",
	); got != "short text with needle" {
		t.Fatalf("short = %q", got)
	}
	// Empty text uses the fallback; empty terms use the leading snippet.
	if got := queryBiasedSnippet("", []string{"x"}, "fallback"); got != "fallback" {
		t.Fatalf("empty = %q", got)
	}
	if got := queryBiasedSnippet(
		strings.Repeat("lead words ", 60),
		nil,
		"f",
	); !strings.HasPrefix(
		got,
		"lead words",
	) {
		t.Fatalf("no terms = %q", got)
	}
	// Blank terms are skipped when anchoring.
	if got := firstTermAnchor("body text", []string{" ", ""}); got != -1 {
		t.Fatalf("blank terms anchor = %d", got)
	}
	// A term early in the text opens the window at the start (no ellipsis).
	early := "needle appears immediately here. " + strings.Repeat("rest of the body words. ", 40)
	if got := queryBiasedSnippet(early, []string{"needle"}, "f"); strings.HasPrefix(got, "…") {
		t.Fatalf("early match must not ellipsize: %q", got)
	}
}

func TestAllowsDocumentDateBounds(t *testing.T) {
	dated := documentstore.Document{
		NormalizedURL: "https://a.example/x",
		FetchedAt:     time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	}
	undated := documentstore.Document{NormalizedURL: "https://a.example/y"}
	minDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	maxDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	if !allowsDocumentDate(dated, SearchRequest{}) {
		t.Fatal("no bounds must pass")
	}
	if !allowsDocumentDate(dated, SearchRequest{MinDate: minDate, MaxDate: maxDate}) {
		t.Fatal("in-range date must pass")
	}
	if allowsDocumentDate(undated, SearchRequest{MinDate: minDate}) {
		t.Fatal("undated must drop under a bound")
	}
	if allowsDocumentDate(
		dated,
		SearchRequest{MinDate: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)},
	) {
		t.Fatal("too-old date must drop")
	}
	if allowsDocumentDate(
		dated,
		SearchRequest{MaxDate: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)},
	) {
		t.Fatal("too-new date must drop")
	}
}

func TestSearchAppliesDateBounds(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://a.example/fresh",
				ExtractedText: "golang fresh document",
				FetchedAt:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			},
			{
				NormalizedURL: "https://a.example/stale",
				ExtractedText: "golang stale document",
				FetchedAt:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query: "golang", MaxResults: 5,
		MinDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.Total != 1 || result.Results[0].URL != "https://a.example/fresh" {
		t.Fatalf("date-bounded results = %+v", result)
	}
}

func TestNewBleveShardRejectsOccupiedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "occupied.idx")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatalf("occupy: %v", err)
	}
	mapping, err := newSearchIndexMapping()
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	// A plain file occupies the path, so bleve.NewUsing must fail.
	if _, err := newBleveShard(path, mapping); err == nil {
		t.Fatal("occupied path must fail shard creation")
	}
}
