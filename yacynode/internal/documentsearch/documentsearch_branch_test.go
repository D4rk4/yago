package documentsearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

func TestIgnoredOptionNamesReportsAcceptedIgnoredFields(t *testing.T) {
	names := ignoredOptionNames(yacyproto.SearchRequest{
		Prefer:         "x",
		Filter:         "host",
		Profile:        "p",
		Author:         "a",
		Collection:     "c",
		FileType:       "pdf",
		Protocol:       "https",
		TimezoneOffset: 1,
	})
	if len(names) != len(ignoredOptions) {
		t.Fatalf("ignored options = %v, want %d entries", names, len(ignoredOptions))
	}
	if got := ignoredOptionNames(yacyproto.SearchRequest{Filter: ".*"}); len(got) != 0 {
		t.Fatalf("ignored options = %v, want none for default filter", got)
	}
}

func TestScanTermCapsKeptMatchesAndSkipsBadPostings(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {
			{WordHash: word, Properties: map[string]string{}},
			postingEntry(word, "u1", 0, 1),
			postingEntry(word, "u2", 0, 1),
		},
	}}
	appearances, total, err := (searcher{index: index, matchesPerTerm: 1}).scanTerm(
		t.Context(),
		word,
		termAppearanceCriteria{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(appearances) != 1 || total != 2 {
		t.Fatalf("appearances=%d total=%d, want 1/2", len(appearances), total)
	}
}

func TestExcludedDocumentsSkipsBadPostings(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {
			{WordHash: word, Properties: map[string]string{}},
			postingEntry(word, "u1", 0, 1),
		},
	}}
	excluded, err := (searcher{index: index}).excludedDocuments(t.Context(), []yacymodel.Hash{word})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := excluded[hashFor("u1")]; !ok {
		t.Fatalf("excluded = %v, want u1", excluded)
	}
}

func TestTranslateAppearanceRejectsBadPostingFields(t *testing.T) {
	if _, ok := translateAppearance(
		t.Context(),
		yacymodel.RWIPosting{Properties: map[string]string{}},
	); ok {
		t.Fatal("posting without URL hash should be rejected")
	}
	entry := postingEntry(hashFor("w1"), "u1", 0, 1)
	entry.Properties[yacymodel.ColHitCount] = "bad"
	if _, ok := translateAppearance(t.Context(), entry); ok {
		t.Fatal("posting with bad hit count should be rejected")
	}
	entry = postingEntry(hashFor("w1"), "u1", 0, 1)
	entry.Properties[yacymodel.ColWordDistance] = "bad"
	if _, ok := translateAppearance(t.Context(), entry); ok {
		t.Fatal("posting with bad word distance should be rejected")
	}
	if _, ok := cardinal(t.Context(), entry, yacymodel.ColWordDistance); ok {
		t.Fatal("bad cardinal should be rejected")
	}
}

func TestReportMatchesBranches(t *testing.T) {
	s := searcher{}
	report, err := s.reportMatches(t.Context(), searchCriteria{}, termMatches{})
	if err != nil {
		t.Fatal(err)
	}
	if report.totalMatchesPerTerm != nil || report.documents != nil {
		t.Fatalf("report = %+v, want empty", report)
	}
	report, err = s.reportMatches(
		t.Context(),
		searchCriteria{reporting: matchReporting{mode: reportingMode(99)}},
		termMatches{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.totalMatchesPerTerm != nil || report.documents != nil {
		t.Fatalf("default report = %+v, want empty", report)
	}

	term := hashFor("w1")
	wanted := termMatches{
		totalMatchesPerTerm: map[yacymodel.Hash]int{term: 1},
		documentsPerTerm:    map[yacymodel.Hash]map[yacymodel.Hash]matchedDocument{},
	}
	report = reportLargestWantedTerm(
		searchCriteria{
			terms:             []yacymodel.Hash{term},
			requiredDocuments: []yacymodel.Hash{hashFor("u1")},
		},
		wanted,
	)
	if report.documents != nil {
		t.Fatalf("documents = %v, want nil when report is suppressed", report.documents)
	}
	report = reportLargestWantedTerm(
		searchCriteria{terms: []yacymodel.Hash{hashFor("w1"), hashFor("w2")}},
		wanted,
	)
	if report.documents != nil {
		t.Fatalf("documents = %v, want nil without selectable term", report.documents)
	}

	first, second := hashFor("a"), hashFor("b")
	selected, ok := termWithMostMatches(map[yacymodel.Hash]map[yacymodel.Hash]matchedDocument{
		second: {hashFor("u2"): {}},
		first:  {hashFor("u1"): {}},
	})
	if !ok || selected != first {
		t.Fatalf("selected = %q/%v, want first term by ascending tie break", selected, ok)
	}
}

func TestMatchingAndOrderingHelpers(t *testing.T) {
	if keepDocumentsMatchingEveryTerm(nil) != nil {
		t.Fatal("empty term list should have no matching documents")
	}
	identifier := hashFor("u1")
	deduped := dedupeDocuments([]termAppearance{
		{documentIdentifier: identifier, occurrences: 1},
		{documentIdentifier: identifier, occurrences: 9},
	})
	if deduped[identifier].occurrences != 1 {
		t.Fatalf(
			"deduped occurrence = %d, want first appearance kept",
			deduped[identifier].occurrences,
		)
	}
	if compareDescending(uint64(3), uint64(2)) != -1 ||
		compareDescending(uint64(2), uint64(3)) != 1 ||
		compareDescending(uint64(2), uint64(2)) != 0 ||
		compareAscending("a", "a") != 0 ||
		compareAscending("b", "a") != 1 {
		t.Fatal("compare helpers returned unexpected ordering")
	}
}

func TestSearchCriteriaRequestBranches(t *testing.T) {
	for domain, want := range map[yacyproto.SearchContentDomain]contentKind{
		yacyproto.ContentDomainImage: imageContent,
		yacyproto.ContentDomainAudio: audioContent,
		yacyproto.ContentDomainVideo: videoContent,
		yacyproto.ContentDomainApp:   applicationContent,
		"":                           anyContent,
	} {
		if got := contentKindFromDomain(domain); got != want {
			t.Fatalf("contentKindFromDomain(%q) = %v, want %v", domain, got, want)
		}
	}
	criteria, err := searchCriteriaFromRequest(yacyproto.SearchRequest{Count: 0, Time: 0})
	if err != nil {
		t.Fatal(err)
	}
	if criteria.maxResults != defaultSearchCount || criteria.timeLimit != defaultSearchTime {
		t.Fatalf("defaults = %d/%s", criteria.maxResults, criteria.timeLimit)
	}
	if _, err := searchCriteriaFromRequest(
		yacyproto.SearchRequest{Constraint: "@@bad@@"},
	); err == nil {
		t.Fatal("expected bad constraint error")
	}
	if _, err := searchCriteriaFromRequest(
		yacyproto.SearchRequest{Modifier: "site:."},
	); err == nil {
		t.Fatal("expected bad site hash error")
	}
	if got := firstNonEmpty("", "", "value"); got != "value" {
		t.Fatalf("firstNonEmpty = %q, want value", got)
	}
}

func TestTermAppearanceCriteriaBranches(t *testing.T) {
	identifier := hashFor("u1")
	criteria := termAppearanceCriteria{
		requiredDocuments: map[yacymodel.Hash]struct{}{identifier: {}},
	}
	if criteria.matches(t.Context(), termAppearance{documentIdentifier: hashFor("u2")}) {
		t.Fatal("required document mismatch should fail")
	}
	criteria = termAppearanceCriteria{
		excludedDocuments: map[yacymodel.Hash]struct{}{identifier: {}},
	}
	if criteria.matches(t.Context(), termAppearance{documentIdentifier: identifier}) {
		t.Fatal("excluded document should fail")
	}
	if matchesSiteHost(t.Context(), yacymodel.URLHash("bad"), "abcdef") {
		t.Fatal("bad URL hash should not match site host")
	}
	criteria = termAppearanceCriteria{siteHash: "abcdef"}
	if criteria.matches(t.Context(), termAppearance{documentLocation: yacymodel.URLHash("bad")}) {
		t.Fatal("site hash mismatch should fail")
	}
	criteria = termAppearanceCriteria{contentKind: audioContent, strictContentKind: true}
	if criteria.matches(t.Context(), appearanceWithContentKind(0)) {
		t.Fatal("content kind mismatch should fail")
	}
	if !matchesContentKind(
		t.Context(),
		appearanceWithContentKind(yacymodel.DocTypeAudio),
		audioContent,
		true,
	) {
		t.Fatal("strict audio should match audio document type")
	}
	if matchesContentKind(t.Context(), appearanceWithContentKind(0), audioContent, true) {
		t.Fatal("strict audio should reject non-audio type")
	}
	if !matchesContentKind(
		t.Context(),
		appearanceWithFlag(yacymodel.RWIFlagHasImage),
		imageContent,
		false,
	) {
		t.Fatal("image flag should match image content")
	}
	required := decodedProperties(t, encodedFlag(yacymodel.RWIFlagHasImage))
	if matchesRequiredProperties(
		t.Context(),
		termAppearance{appearanceFlagsError: errors.New("bad flags")},
		required,
	) {
		t.Fatal("bad appearance flags should fail required property match")
	}
}

func TestEndpointReturnsCriteriaAndSearchErrors(t *testing.T) {
	endpoint := searchEndpoint{identity: searchIdentity(), searcher: searcher{}}
	if _, err := endpoint.Serve(t.Context(), yacyproto.SearchRequest{
		NetworkName: "freeworld",
		Constraint:  "@@bad@@",
	}); err == nil {
		t.Fatal("expected criteria error")
	}

	endpoint = searchEndpoint{
		identity: searchIdentity(),
		searcher: searcher{index: fakeScanner{err: errors.New("scan failed")}},
	}
	if _, err := endpoint.Serve(t.Context(), yacyproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yacymodel.Hash{hashFor("w1")},
	}); err == nil {
		t.Fatal("expected search error")
	}
}

func TestEndpointLogsIgnoredOptionsAndNoTimeout(t *testing.T) {
	word := hashFor("w1")
	endpoint := newEndpoint(
		fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
			word: {postingEntry(word, "u1", 0, 1)},
		}},
		fakeDirectory{rows: urlRows("u1")},
	)
	resp, err := endpoint(t.Context(), yacyproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yacymodel.Hash{word},
		Count:       1,
		Time:        -1,
		Prefer:      "preferred",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Count != 1 {
		t.Fatalf("Count = %d, want 1", resp.Count)
	}
}

type searchWireStatus struct{}

func (searchWireStatus) Version(context.Context) string { return "1.940" }
func (searchWireStatus) Uptime(context.Context) int     { return 42 }

func TestMountSearchServesRoute(t *testing.T) {
	mux := http.NewServeMux()
	word := hashFor("w1")
	MountSearch(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(searchWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		searchIdentity(),
		fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
			word: {postingEntry(word, "u1", 0, 1)},
		}},
		fakeDirectory{rows: urlRows("u1")},
		100,
	)
	req := yacyproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yacymodel.Hash{word},
		Count:       1,
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yacyproto.PathSearch+"?"+req.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}
