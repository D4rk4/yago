package searchindex

import (
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestBleveDiskCandidateSearchUsesOnlyStoredProjection(t *testing.T) {
	published := time.Date(2025, 6, 7, 0, 0, 0, 0, time.UTC)
	body := "needle " + strings.Repeat("body ", 100_000)
	images := make([]documentstore.ImageMetadata, maximumStoredCandidateImages+3)
	for index := range images {
		images[index] = documentstore.ImageMetadata{
			URL:     "https://images.example/" + string(rune('a'+index)) + ".png",
			AltText: "needle image",
		}
	}
	eligible := documentstore.Document{
		NormalizedURL:  "https://example.org/eligible.png",
		Title:          "Needle result",
		ExtractedText:  body,
		ContentQuality: documentstore.ContentQualityEvidence{Known: true, Score: 0.75},
		ContentSafety: documentstore.ContentSafetyEvidence{
			Rating: documentstore.SafetyGeneral,
		},
		Language:       "en",
		ContentType:    "image/png",
		PublishedAt:    published,
		DateConfidence: 0.8,
		ClusterID:      "cluster",
		Images:         images,
		Metadata:       map[string]string{"author": "Ada Lovelace"},
	}
	explicit := eligible
	explicit.NormalizedURL = "https://example.org/explicit.png"
	explicit.ContentSafety.Rating = documentstore.SafetyExplicit
	german := eligible
	german.NormalizedURL = "https://example.org/german.png"
	german.Language = "de"
	documents := []documentstore.Document{eligible, explicit, german}
	directory := newFakeDocumentDirectory(documents...)
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "candidate.bleve"),
		directory,
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	directory.loads = 0
	directory.err = errors.New("full document loaded")

	result, err := index.Search(t.Context(), SearchRequest{
		Query:         "needle",
		MaxResults:    1,
		CandidateOnly: true,
		IncludeRaw:    true,
		SafeSearch:    true,
		Language:      "en",
		Author:        "lovelace",
		WithFacets:    true,
		ContentDomain: "image",
		FileType:      "png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if directory.loads != 0 {
		t.Fatalf("document loads = %d", directory.loads)
	}
	if result.Total != 1 || len(result.Results) != 1 {
		t.Fatalf("result = %#v", result)
	}
	got := result.Results[0]
	if got.URL != eligible.NormalizedURL || got.Title != eligible.Title ||
		got.RawContent != "" || got.Size != len(body) || got.Quality != 0.75 ||
		got.Author != "Ada Lovelace" || got.PublishedDate != published ||
		len(got.Images) != maximumStoredCandidateImages {
		t.Fatalf("candidate = %#v", got)
	}
	if len(got.Snippet) > maximumStoredCandidateSnippetBytes ||
		facetTermCounts(result.Facets, "language")["en"] != 1 ||
		facetTermCounts(result.Facets, "month")["2025-06"] != 1 {
		t.Fatalf("candidate evidence = %#v facets=%#v", got, result.Facets)
	}
}

func TestStoredCandidateProjectionBoundsAndCompleteness(t *testing.T) {
	cutRune := strings.Repeat("a", maximumStoredCandidateTitleBytes-1) + "Жtail"
	bounded, complete := boundedStoredCandidateString(
		cutRune,
		maximumStoredCandidateTitleBytes,
	)
	if complete || len(bounded) > maximumStoredCandidateTitleBytes ||
		!utf8.ValidString(bounded) {
		t.Fatalf("bounded string = %q complete=%t", bounded, complete)
	}
	cluster := strings.Repeat("cluster", maximumStoredCandidateClusterBytes)
	if identity := boundedStoredCandidateClusterID(cluster); len(identity) != 64 ||
		identity != boundedStoredCandidateClusterID(cluster) {
		t.Fatalf("cluster identity = %q", identity)
	}
	if boundedStoredCandidateClusterID("short") != "short" {
		t.Fatal("short cluster identity changed")
	}
	images := make([]documentstore.ImageMetadata, maximumStoredCandidateImages+1)
	for index := range images {
		images[index] = documentstore.ImageMetadata{
			URL:     strings.Repeat("u", maximumStoredCandidateImageURLBytes+1),
			AltText: strings.Repeat("a", maximumStoredCandidateImageAltBytes+1),
		}
	}
	projection := newStoredCandidateProjection(documentstore.Document{
		Title:             cutRune,
		ExtractedText:     strings.Repeat("body ", 1000),
		ClusterID:         cluster,
		RepresentativeURL: strings.Repeat("r", maximumStoredCandidateRepresentativeBytes+1),
		Language:          strings.Repeat("l", maximumStoredCandidateLanguageBytes+1),
		ContentType:       strings.Repeat("m", maximumStoredCandidateContentTypeBytes+1),
		Images:            images,
		Metadata: map[string]string{
			"author":    strings.Repeat("a", maximumStoredCandidateAuthorBytes+1),
			"keywords":  strings.Repeat("k", maximumStoredCandidateKeywordsBytes+1),
			"publisher": strings.Repeat("p", maximumStoredCandidatePublisherBytes+1),
		},
	})
	if len(projection.Title) > maximumStoredCandidateTitleBytes ||
		len(projection.Snippet) > maximumStoredCandidateSnippetBytes ||
		len(projection.RepresentativeURL) > maximumStoredCandidateRepresentativeBytes ||
		len(projection.Author) > maximumStoredCandidateAuthorBytes ||
		len(projection.Keywords) > maximumStoredCandidateKeywordsBytes ||
		len(projection.Publisher) > maximumStoredCandidatePublisherBytes ||
		len(projection.Language) > maximumStoredCandidateLanguageBytes ||
		len(projection.ContentType) > maximumStoredCandidateContentTypeBytes ||
		len(projection.Images) != maximumStoredCandidateImages ||
		projection.RepresentativeComplete || projection.AuthorComplete ||
		projection.LanguageComplete || projection.ContentTypeComplete {
		t.Fatalf("projection = %#v", projection)
	}
	for _, image := range projection.Images {
		if len(image.URL) > maximumStoredCandidateImageURLBytes ||
			len(image.AltText) > maximumStoredCandidateImageAltBytes {
			t.Fatalf("image = %#v", image)
		}
	}
	document := (storedCandidateProjection{HasImages: true}).document("document")
	if len(document.Images) != 1 || document.NormalizedURL != "document" {
		t.Fatalf("projected document = %#v", document)
	}
}

func TestStoredCandidateProjectionFilterCompleteness(t *testing.T) {
	completeProjection := storedCandidateProjection{
		RepresentativeComplete: true,
		AuthorComplete:         true,
		LanguageComplete:       true,
		ContentTypeComplete:    true,
	}
	if !completeProjection.supports(SearchRequest{}) ||
		completeProjection.supports(SearchRequest{Near: true}) {
		t.Fatal("near projection support is incorrect")
	}
	completeProjection.AuthorComplete = false
	if completeProjection.supports(SearchRequest{Author: "author"}) {
		t.Fatal("truncated author accepted")
	}
	completeProjection.AuthorComplete = true
	completeProjection.LanguageComplete = false
	if completeProjection.supports(SearchRequest{Language: "en"}) {
		t.Fatal("truncated language accepted")
	}
	completeProjection.LanguageComplete = true
	completeProjection.ContentTypeComplete = false
	for _, request := range []SearchRequest{
		{FileType: "pdf"},
		{ContentDomain: "audio"},
		{ContentDomain: "VIDEO"},
		{ContentDomain: "app"},
	} {
		if completeProjection.supports(request) {
			t.Fatalf("truncated content type accepted for %#v", request)
		}
	}
	if !completeProjection.supports(SearchRequest{ContentDomain: "image"}) {
		t.Fatal("image projection incorrectly needs content type")
	}
	completeProjection.RepresentativeComplete = false
	if completeProjection.supports(SearchRequest{}) {
		t.Fatal("truncated representative accepted")
	}
}

func TestStoredCandidateProjectionDecodeAndFieldSelection(t *testing.T) {
	if _, err := decodeStoredCandidateProjection(&search.DocumentMatch{}); err == nil {
		t.Fatal("missing projection accepted")
	}
	if _, err := decodeStoredCandidateProjection(&search.DocumentMatch{Fields: map[string]any{
		storedCandidateField: "{",
	}}); err == nil {
		t.Fatal("malformed projection accepted")
	}
	encoded, err := encodeStoredCandidateProjection(documentstore.Document{Title: "title"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStoredCandidateProjection(&search.DocumentMatch{Fields: map[string]any{
		storedCandidateField: encoded,
	}})
	if err != nil || decoded.Title != "title" {
		t.Fatalf("decoded = %#v, %v", decoded, err)
	}
	if fields := storedSearchFields(SearchRequest{}, true); len(fields) != 1 ||
		fields[0] != documentAnalyzerField {
		t.Fatalf("ordinary fields = %#v", fields)
	}
	if fields := storedSearchFields(
		SearchRequest{CandidateOnly: true},
		true,
	); len(fields) != 2 ||
		fields[1] != storedCandidateField {
		t.Fatalf("candidate fields = %#v", fields)
	}
	if fields := storedSearchFields(SearchRequest{CandidateOnly: true}, false); len(fields) != 1 {
		t.Fatalf("legacy fields = %#v", fields)
	}
}

func TestStoredCandidateProjectionPresenceAndCompatibilityFallback(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/candidate",
		Title:         "candidate",
		ExtractedText: "needle body",
	}
	encoded, err := encodeStoredCandidateProjection(doc)
	if err != nil {
		t.Fatal(err)
	}
	directory := newFakeDocumentDirectory(doc)
	index := &BleveDiskIndex{
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
	request := SearchRequest{CandidateOnly: true}
	for name, fields := range map[string]map[string]any{
		"missing":   {},
		"malformed": {storedCandidateField: "{"},
	} {
		t.Run(name, func(t *testing.T) {
			directory.loads = 0
			projection, found, err := index.loadSearchHitProjection(
				t.Context(),
				&search.DocumentMatch{ID: doc.NormalizedURL, Fields: fields},
				request,
			)
			if err != nil || !found || projection.candidate || directory.loads != 1 {
				t.Fatalf(
					"projection=%#v found=%t loads=%d err=%v",
					projection,
					found,
					directory.loads,
					err,
				)
			}
		})
	}
	hit := &search.DocumentMatch{
		ID: doc.NormalizedURL,
		Fields: map[string]any{
			storedCandidateField: encoded,
		},
	}
	directory.loads = 0
	delete(directory.documents, doc.NormalizedURL)
	_, found, err := index.loadSearchHitProjection(t.Context(), hit, request)
	if err != nil || found || directory.loads != 0 {
		t.Fatalf("missing projection found=%t loads=%d err=%v", found, directory.loads, err)
	}
	directory.documents[doc.NormalizedURL] = doc
	directory.presenceErr = errors.New("presence failed")
	if _, _, err := index.loadSearchHitProjection(
		t.Context(),
		hit,
		request,
	); !errors.Is(err, directory.presenceErr) {
		t.Fatalf("presence error = %v", err)
	}
	directory.presenceErr = nil
	index.documentPresence = nil
	directory.loads = 0
	projection, found, err := index.loadSearchHitProjection(t.Context(), hit, request)
	if err != nil || !found || projection.candidate || directory.loads != 1 {
		t.Fatalf(
			"compatibility projection=%#v found=%t loads=%d err=%v",
			projection,
			found,
			directory.loads,
			err,
		)
	}
}

func TestBleveDiskCandidateSearchCleansPresenceOrphan(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/orphan",
		ExtractedText: "needle",
	}
	directory := newFakeDocumentDirectory(doc)
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "orphan.bleve"),
		directory,
		&fakeStoredDocuments{documents: []documentstore.Document{doc}},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	delete(directory.documents, doc.NormalizedURL)
	directory.loads = 0

	result, err := index.Search(t.Context(), SearchRequest{
		Query: "needle", MaxResults: 1, CandidateOnly: true,
	})
	if err != nil || result.Total != 0 || len(result.Results) != 0 || directory.loads != 0 {
		t.Fatalf("result=%#v loads=%d err=%v", result, directory.loads, err)
	}
	stats, err := index.Stats(t.Context())
	if err != nil || stats.Documents != 0 {
		t.Fatalf("stats=%#v err=%v", stats, err)
	}
}

func TestStoredCandidateProjectionFallsBackForExactAuthorFilter(t *testing.T) {
	author := strings.Repeat("a", maximumStoredCandidateAuthorBytes) + "Exact Tail"
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/author",
		ExtractedText: "needle",
		Metadata:      map[string]string{"author": author},
	}
	directory := newFakeDocumentDirectory(doc)
	index, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "author.bleve"),
		directory,
		&fakeStoredDocuments{documents: []documentstore.Document{doc}},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	directory.loads = 0

	result, err := index.Search(t.Context(), SearchRequest{
		Query: "needle", MaxResults: 1, CandidateOnly: true, Author: "exact tail",
	})
	if err != nil || result.Total != 1 || len(result.Results) != 1 || directory.loads == 0 {
		t.Fatalf("result=%#v loads=%d err=%v", result, directory.loads, err)
	}
}

func TestCandidateEncodingErrorsStopEveryIndexPath(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/invalid",
		ExtractedText: "needle",
		ContentQuality: documentstore.ContentQualityEvidence{
			Score: math.NaN(),
		},
	}
	if _, err := bleveDocumentFromStore(doc); err == nil {
		t.Fatal("invalid candidate encoded")
	}
	memory, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = memory.index.Close() })
	if err := memory.Index(t.Context(), doc); err == nil {
		t.Fatal("memory index accepted invalid candidate")
	}
	disk, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "invalid.bleve"),
		newFakeDocumentDirectory(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disk.Close() })
	if err := disk.Index(t.Context(), doc); err == nil {
		t.Fatal("disk index accepted invalid candidate")
	}
	if err := disk.IndexBatch(t.Context(), []documentstore.Document{doc}); err == nil {
		t.Fatal("batch index accepted invalid candidate")
	}
}
