package documentsearch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
	"github.com/D4rk4/yago/yagoproto"
)

func TestNegotiatedAnalyzerRecallFindsPeerOnlySiblingInflections(t *testing.T) {
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rawURL := "https://peer.example/authority"
	if err := index.Index(t.Context(), documentstore.Document{
		NormalizedURL: rawURL,
		Title:         "Чрезвычайные полномочия",
		ExtractedText: "порядок чрезвычайных полномочий государства",
		Language:      "ru",
	}); err != nil {
		t.Fatal(err)
	}
	urlHash, _ := yagomodel.HashURL(rawURL)
	analyzerRow := metadataRow(t, urlHash.Hash(), rawURL, "Чрезвычайные полномочия")
	legacyHash := hashFor("legacy")
	legacyRow := metadataRow(t, legacyHash, "https://legacy.example/", "Legacy")
	source := negotiatedAnalyzerRecallSource{
		searcher: searchlocal.NewSearcher(index),
		documents: fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
			urlHash.Hash(): analyzerRow,
			legacyHash:     legacyRow,
		}},
	}
	current := searchResult{
		resources:                       []yagomodel.URIMetadataRow{legacyRow},
		totalDocumentsMatchingEveryTerm: 1,
	}
	merged, err := source.merge(t.Context(), yagoproto.SearchRequest{
		Count:      10,
		ContentDom: yagoproto.ContentDomainText,
		Query: []yagomodel.Hash{
			yagomodel.WordHash("чрезвычайные"),
			yagomodel.WordHash("полномочия"),
		},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"чрезвычайные", "полномочия"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.resources) != 2 || merged.resources[0].Properties[yagomodel.URLMetaHash] !=
		urlHash.String() || merged.totalDocumentsMatchingEveryTerm != 2 {
		t.Fatalf("merged analyzer result = %#v", merged)
	}
}

func TestNegotiatedAnalyzerRecallPreservesLegacyFallbacksAndErrors(t *testing.T) {
	current := searchResult{totalDocumentsMatchingEveryTerm: 3}
	request := yagoproto.SearchRequest{
		Query:           []yagomodel.Hash{yagomodel.WordHash("term")},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"term"},
	}
	for index, source := range []negotiatedAnalyzerRecallSource{
		{},
		{searcher: analyzerRecallSearcher{}, documents: nil},
		{searcher: analyzerRecallSearcher{}, documents: fakeDirectory{}},
	} {
		candidate := request
		if index == 2 {
			candidate.EvidenceVersion++
		}
		got, err := source.merge(t.Context(), candidate, current)
		if err != nil || got.totalDocumentsMatchingEveryTerm != 3 {
			t.Fatalf("fallback %d = %#v, %v", index, got, err)
		}
	}
	boom := errors.New("boom")
	failedIndex := negotiatedAnalyzerRecallSource{
		searcher: analyzerRecallSearcher{err: boom}, documents: fakeDirectory{},
	}
	if got, err := failedIndex.merge(t.Context(), request, current); !errors.Is(err, boom) ||
		got.totalDocumentsMatchingEveryTerm != 3 {
		t.Fatalf("index failure = %#v, %v", got, err)
	}
	rawURL := "https://peer.example/result"
	failedDirectory := negotiatedAnalyzerRecallSource{
		searcher: analyzerRecallSearcher{response: searchcore.Response{
			Results: []searchcore.Result{{URL: rawURL}}, TotalResults: 1,
		}},
		documents: fakeDirectory{err: boom},
	}
	if got, err := failedDirectory.merge(t.Context(), request, current); !errors.Is(err, boom) ||
		got.totalDocumentsMatchingEveryTerm != 3 {
		t.Fatalf("directory failure = %#v, %v", got, err)
	}
}

func TestNegotiatedAnalyzerRecallEligibilityAndRequestBounds(t *testing.T) {
	base := yagoproto.SearchRequest{
		Query: []yagomodel.Hash{
			yagomodel.WordHash("first"),
			yagomodel.WordHash("second"),
		},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"first", "second"},
	}
	if !negotiatedAnalyzerRecallEligible(base) {
		t.Fatal("negotiated request was not eligible")
	}
	invalid := []yagoproto.SearchRequest{base, base, base, base, base, base, base, base, base}
	invalid[0].EvidenceVersion = 0
	invalid[1].EvidenceTerms = nil
	invalid[2].Exclude = []yagomodel.Hash{hashFor("excluded")}
	invalid[3].Abstracts = yagoproto.SearchAbstracts(hashFor("abstract").String())
	invalid[4].SiteHash = hashFor("site").String()
	invalid[5].Constraint = "constraint"
	invalid[6].Protocol = "https"
	invalid[7].Query = nil
	invalid[8].EvidenceTerms = []string{"first", "other"}
	for position, request := range invalid {
		if negotiatedAnalyzerRecallEligible(request) {
			t.Fatalf("ineligible request %d was accepted", position)
		}
	}
	secondary := base
	secondary.Query = nil
	secondary.URLs = []yagomodel.Hash{hashFor("allowed")}
	if !negotiatedAnalyzerRecallEligible(secondary) {
		t.Fatal("URL-bounded secondary request was rejected")
	}
	duplicate := base
	duplicate.Query = []yagomodel.Hash{
		yagomodel.WordHash("first"),
		yagomodel.WordHash("first"),
	}
	duplicate.EvidenceTerms = []string{"first", "first"}
	if !negotiatedAnalyzerRecallEligible(duplicate) {
		t.Fatal("matching duplicate requirements were rejected")
	}
	duplicate.Query[1] = yagomodel.WordHash("second")
	if negotiatedAnalyzerRecallEligible(duplicate) {
		t.Fatal("over-repeated evidence requirement was accepted")
	}
	request := base
	request.Modifier = "/language/ru site:modifier.example"
	request.SiteHost = " explicit.example "
	request.StrictContentDom = true
	request.ContentDom = yagoproto.ContentDomainImage
	request.FileType = "pdf"
	got := negotiatedAnalyzerSearchRequest(request, 20)
	if got.Query != "first second" || len(got.Terms) != 2 || got.Limit != 32 ||
		got.Source != searchcore.SourceLocal || got.Language != "ru" ||
		got.ContentDomain != searchcore.ContentDomainImage || got.FileType != "pdf" ||
		got.SiteHost != "explicit.example" {
		t.Fatalf("analyzer request = %#v", got)
	}
	request.SiteHost = ""
	request.StrictContentDom = false
	got = negotiatedAnalyzerSearchRequest(request, 5)
	if got.Limit != 10 || got.ContentDomain != "" || got.SiteHost != "modifier.example" {
		t.Fatalf("fallback analyzer request = %#v", got)
	}
	if firstNonempty(" ", "") != "" || firstNonempty(" ", " value ") != "value" {
		t.Fatal("first nonempty value normalization failed")
	}
}

func TestNegotiatedAnalyzerRecallRowsAndMergeAreBounded(t *testing.T) {
	firstURL := "https://peer.example/first"
	secondURL := "https://peer.example/second"
	firstHash, _ := yagomodel.HashURL(firstURL)
	secondHash, _ := yagomodel.HashURL(secondURL)
	firstRow := metadataRow(t, firstHash.Hash(), firstURL, "First")
	secondRow := metadataRow(t, secondHash.Hash(), secondURL, "Second")
	source := negotiatedAnalyzerRecallSource{documents: fakeDirectory{
		rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
			firstHash.Hash():  firstRow,
			secondHash.Hash(): secondRow,
		},
	}}
	rows, err := source.rows(
		t.Context(),
		[]yagomodel.Hash{secondHash.Hash()},
		[]searchcore.Result{
			{URL: firstURL}, {URL: secondURL}, {URL: secondURL},
		},
	)
	if err != nil || len(rows) != 1 ||
		rows[0].Properties[yagomodel.URLMetaHash] != secondHash.String() {
		t.Fatalf("filtered rows = %#v, %v", rows, err)
	}
	malformed := yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash: "bad",
	}}
	merged := mergeAnalyzerRows(
		[]yagomodel.URIMetadataRow{malformed, firstRow, secondRow},
		[]yagomodel.URIMetadataRow{firstRow},
		1,
	)
	if len(merged) != 1 || merged[0].Properties[yagomodel.URLMetaHash] != firstHash.String() {
		t.Fatalf("bounded rows = %#v", merged)
	}
	merged = mergeAnalyzerRows([]yagomodel.URIMetadataRow{firstRow}, []yagomodel.URIMetadataRow{
		firstRow, secondRow,
	}, 3)
	if len(merged) != 2 {
		t.Fatalf("deduplicated rows = %#v", merged)
	}
}

type analyzerRecallSearcher struct {
	response searchcore.Response
	err      error
	wait     time.Duration
}

func (s analyzerRecallSearcher) Search(
	ctx context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	if s.wait > 0 {
		select {
		case <-time.After(s.wait):
		case <-ctx.Done():
			return searchcore.Response{}, fmt.Errorf("analyzer recall wait: %w", ctx.Err())
		}
	}
	return s.response, s.err
}

func TestNegotiatedAnalyzerRecallAppliesItsOwnDeadline(t *testing.T) {
	source := negotiatedAnalyzerRecallSource{
		searcher: analyzerRecallSearcher{wait: time.Second}, documents: fakeDirectory{},
		duration: time.Millisecond,
	}
	_, err := source.merge(t.Context(), yagoproto.SearchRequest{
		Query:           []yagomodel.Hash{yagomodel.WordHash("term")},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"term"},
	}, searchResult{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
}

func TestSearchEndpointKeepsRWIResponseWhenAnalyzerRecallFails(t *testing.T) {
	endpoint := searchEndpoint{
		identity: searchIdentity(),
		searcher: searcher{
			index:          fakeScanner{},
			documents:      fakeDirectory{},
			matchesPerTerm: 10,
		},
		analyzerRecall: negotiatedAnalyzerRecallSource{
			searcher: analyzerRecallSearcher{response: searchcore.Response{
				Results: []searchcore.Result{{URL: "https://peer.example/result"}},
			}},
			documents: fakeDirectory{err: errors.New("metadata unavailable")},
		},
	}
	response, err := endpoint.Serve(t.Context(), yagoproto.SearchRequest{
		NetworkName:     "freeworld",
		Query:           []yagomodel.Hash{yagomodel.WordHash("term")},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"term"},
	})
	if err != nil || response.Count != 0 {
		t.Fatalf("fallback response = %#v, %v", response, err)
	}
}

func BenchmarkNegotiatedAnalyzerRecall(b *testing.B) {
	index, err := searchindex.NewBleveMemoryIndex(b.Context(), nil)
	if err != nil {
		b.Fatal(err)
	}
	rows := make(map[yagomodel.Hash]yagomodel.URIMetadataRow, 100)
	for position := range 100 {
		rawURL := "https://peer.example/" + strconv.Itoa(position)
		document := documentstore.Document{
			NormalizedURL: rawURL,
			Title:         "Чрезвычайные полномочия " + strconv.Itoa(position),
			ExtractedText: "порядок чрезвычайных полномочий государства",
			Language:      "ru",
		}
		if err := index.Index(b.Context(), document); err != nil {
			b.Fatal(err)
		}
		hash, _ := yagomodel.HashURL(rawURL)
		rows[hash.Hash()] = metadataRow(b, hash.Hash(), rawURL, document.Title)
	}
	source := negotiatedAnalyzerRecallSource{
		searcher: searchlocal.NewSearcher(index), documents: fakeDirectory{rows: rows},
	}
	request := yagoproto.SearchRequest{
		Count: 10,
		Query: []yagomodel.Hash{
			yagomodel.WordHash("чрезвычайные"),
			yagomodel.WordHash("полномочия"),
		},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"чрезвычайные", "полномочия"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := source.merge(b.Context(), request, searchResult{}); err != nil {
			b.Fatal(err)
		}
	}
}
