package searchindex

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestDocumentPassageTargetsLongRussianMorphologyEvidence(t *testing.T) {
	prefix := strings.Repeat("Дальний вводный материал без совпадения. ", 400)
	witness := "Передача чрезвычайных полномочий завершена."
	body := prefix + witness
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/long-russian",
		Title:         "Архив",
		ExtractedText: body,
		Language:      "ru",
	}
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{doc},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	result, err := index.Search(t.Context(), SearchRequest{
		Query:      "чрезвычайные полномочия",
		Terms:      []string{"чрезвычайные", "полномочия"},
		MaxResults: 1,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Results) != 1 || len(result.Results[0].BodyQueryMatches) != 2 {
		t.Fatalf("results = %#v", result.Results)
	}
	matched := make([]string, len(result.Results[0].BodyQueryMatches))
	for index, match := range result.Results[0].BodyQueryMatches {
		matched[index] = body[match.Start:match.End]
		if match.Start < len(prefix) {
			t.Fatalf("body match stayed in leading snippet: %#v", match)
		}
	}
	if strings.Join(matched, " ") != "чрезвычайных полномочий" {
		t.Fatalf("body matches = %q", matched)
	}
	start := strings.Index(body, witness)
	passage, found, err := index.DocumentPassage(t.Context(), DocumentPassageRequest{
		DocumentID: doc.NormalizedURL,
		Analyzer:   result.Results[0].Analyzer,
		Terms:      []string{"чрезвычайные", "полномочия"},
		Start:      start,
		End:        start + len(witness),
	})
	if err != nil || !found {
		t.Fatalf("DocumentPassage: found=%t error=%v", found, err)
	}
	if passage.Text != witness || passage.Start != start || passage.End != start+len(witness) {
		t.Fatalf("passage = %#v", passage)
	}
	passageMatches := make([]string, len(passage.QueryMatches))
	for index, match := range passage.QueryMatches {
		passageMatches[index] = passage.Text[match.Start:match.End]
	}
	if strings.Join(passageMatches, " ") != "чрезвычайных полномочий" {
		t.Fatalf("passage matches = %q", passageMatches)
	}
}

func TestDocumentPassageBoundsTextAndMatches(t *testing.T) {
	body := strings.Repeat("term ", maximumDocumentPassageRunes+100)
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/repeated",
		ExtractedText: body,
		Language:      "en",
	}
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{doc},
	})
	if err != nil {
		t.Fatal(err)
	}
	passage, found, err := index.DocumentPassage(t.Context(), DocumentPassageRequest{
		DocumentID: doc.NormalizedURL,
		Analyzer:   "en",
		Terms:      []string{"term"},
		Start:      0,
		End:        len(body),
	})
	if err != nil || !found {
		t.Fatalf("DocumentPassage: found=%t error=%v", found, err)
	}
	if utf8.RuneCountInString(passage.Text) != maximumDocumentPassageRunes ||
		passage.End >= len(body) || len(passage.QueryMatches) != maximumAnalyzedQueryMatches {
		t.Fatalf("bounded passage = %#v", passage)
	}
}

func TestDocumentPassageValidationAndCancellation(t *testing.T) {
	doc := documentstore.Document{NormalizedURL: "doc", ExtractedText: "é term"}
	valid := DocumentPassageRequest{
		DocumentID: "doc",
		Analyzer:   "en",
		Terms:      []string{"term"},
		Start:      0,
		End:        len(doc.ExtractedText),
	}
	for _, req := range invalidDocumentPassageRequests(valid, doc) {
		_, err := documentPassage(t.Context(), doc, req)
		var invalid interface {
			DocumentPassageRequestInvalid()
		}
		if err == nil || err.Error() == "" || !errors.As(err, &invalid) {
			t.Fatalf("invalid request accepted: %#v", req)
		}
		invalid.DocumentPassageRequestInvalid()
	}
	assertDocumentPassageCancellation(t, doc, valid)
}

func invalidDocumentPassageRequests(
	valid DocumentPassageRequest,
	doc documentstore.Document,
) []DocumentPassageRequest {
	return []DocumentPassageRequest{
		{Terms: valid.Terms, Start: 0, End: 1},
		{DocumentID: "bad\xff", Analyzer: "en", Terms: valid.Terms, Start: 0, End: 1},
		{DocumentID: "doc", Start: 0, End: 1},
		{DocumentID: "doc", Terms: valid.Terms, Start: 0, End: len(doc.ExtractedText)},
		{
			DocumentID: "doc",
			Analyzer:   "unknown",
			Terms:      valid.Terms,
			Start:      0,
			End:        len(doc.ExtractedText),
		},
		{
			DocumentID: "doc",
			Analyzer:   "en",
			Terms:      []string{""},
			Start:      0,
			End:        len(doc.ExtractedText),
		},
		{
			DocumentID: "doc",
			Analyzer:   "en",
			Terms:      []string{"bad\xff"},
			Start:      0,
			End:        len(doc.ExtractedText),
		},
		{
			DocumentID: "doc",
			Analyzer:   "en",
			Terms:      make([]string, maximumDocumentPassageTerms+1),
			Start:      0,
			End:        1,
		},
		{
			DocumentID:       "doc",
			Analyzer:         "en",
			Terms:            valid.Terms,
			Start:            0,
			End:              1,
			SurroundingRunes: -1,
		},
		{
			DocumentID:       "doc",
			Analyzer:         "en",
			Terms:            valid.Terms,
			Start:            0,
			End:              1,
			SurroundingRunes: maximumDocumentPassageSurroundingRunes + 1,
		},
		{DocumentID: "doc", Analyzer: "en", Terms: valid.Terms, Start: -1, End: 1},
		{DocumentID: "doc", Analyzer: "en", Terms: valid.Terms, Start: 1, End: 2},
		{
			DocumentID: "doc",
			Analyzer:   "en",
			Terms:      valid.Terms,
			Start:      0,
			End:        len(doc.ExtractedText) + 1,
		},
	}
}

func assertDocumentPassageCancellation(
	t *testing.T,
	doc documentstore.Document,
	valid DocumentPassageRequest,
) {
	t.Helper()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := documentPassage(canceled, doc, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-analysis cancellation = %v", err)
	}
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{doc},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := index.DocumentPassage(canceled, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-load cancellation = %v", err)
	}
	invalid := valid
	invalid.Terms = nil
	if _, found, err := index.DocumentPassage(t.Context(), invalid); err == nil || found {
		t.Fatalf("invalid stored passage: found=%t error=%v", found, err)
	}
	valid.DocumentID = "missing"
	if _, found, err := index.DocumentPassage(t.Context(), valid); err != nil || found {
		t.Fatalf("missing passage: found=%t error=%v", found, err)
	}
}

func TestDocumentPassageAddsUTF8SafeSurroundingText(t *testing.T) {
	body := "вводные сведения передача полномочий завершена далее"
	start := strings.Index(body, "полномочий")
	end := start + len("полномочий")
	passage, err := documentPassage(t.Context(), documentstore.Document{
		ExtractedText: body,
		Language:      "ru",
	}, DocumentPassageRequest{
		DocumentID:       "document",
		Analyzer:         "ru",
		Terms:            []string{"полномочия"},
		Start:            start,
		End:              end,
		SurroundingRunes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if passage.Start >= start || passage.End <= end ||
		passage.Text == "полномочий" || !utf8.ValidString(passage.Text) ||
		len(passage.QueryMatches) != 1 ||
		passage.Text[passage.QueryMatches[0].Start:passage.QueryMatches[0].End] != "полномочий" {
		t.Fatalf("contextual passage = %#v", passage)
	}
}

func TestDiskAndCachedDocumentPassageLifecycle(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/disk-passage",
		ExtractedText: "prefix matching evidence suffix",
		Language:      "en",
	}
	directory := newFakeDocumentDirectory(doc)
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		directory,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	cache := NewCachedSearchIndex(index, 1)
	req := DocumentPassageRequest{
		DocumentID: doc.NormalizedURL,
		Analyzer:   "en",
		Terms:      []string{"matching"},
		Start:      len("prefix "),
		End:        len("prefix matching evidence"),
	}
	passage, found, err := cache.DocumentPassage(t.Context(), req)
	if err != nil || !found || passage.Text != "matching evidence" ||
		len(passage.QueryMatches) != 1 {
		t.Fatalf("cached passage=%#v found=%t error=%v", passage, found, err)
	}
	directory.err = errors.New("directory unavailable")
	if _, _, err := cache.DocumentPassage(t.Context(), req); err == nil {
		t.Fatal("directory failure was hidden")
	}
	directory.err = nil
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := index.DocumentPassage(t.Context(), req); err == nil {
		t.Fatal("closed index served a passage")
	}
	if _, _, err := NewCachedSearchIndex(&countingIndex{}, 1).DocumentPassage(
		t.Context(),
		req,
	); err == nil {
		t.Fatal("unsupported cached index served a passage")
	}
}

func BenchmarkDocumentPassageRussianMorphology(b *testing.B) {
	body := strings.Repeat("вводные сведения ", maximumDocumentPassageRunes/2) +
		"передача чрезвычайных полномочий"
	doc := documentstore.Document{ExtractedText: body, Language: "ru"}
	start := len(body) - len("передача чрезвычайных полномочий")
	req := DocumentPassageRequest{
		DocumentID:       "document",
		Analyzer:         "ru",
		Terms:            []string{"чрезвычайные", "полномочия"},
		Start:            start,
		End:              len(body),
		SurroundingRunes: maximumDocumentPassageSurroundingRunes / 2,
	}
	b.ReportAllocs()
	for b.Loop() {
		passage, err := documentPassage(b.Context(), doc, req)
		if err != nil || len(passage.QueryMatches) != 2 {
			b.Fatalf("passage=%#v error=%v", passage, err)
		}
	}
}
