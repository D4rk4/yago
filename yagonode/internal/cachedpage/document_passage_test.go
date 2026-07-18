package cachedpage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
)

type fakeDocumentPassageSearcher struct {
	passage searchcore.DocumentPassage
	found   bool
	err     error
	request searchcore.DocumentPassageRequest
}

func (s *fakeDocumentPassageSearcher) DocumentPassage(
	_ context.Context,
	request searchcore.DocumentPassageRequest,
) (searchcore.DocumentPassage, bool, error) {
	s.request = request

	return s.passage, s.found, s.err
}

func getWithDocumentPassages(
	t *testing.T,
	directory documentstore.DocumentDirectory,
	passages searchcore.DocumentPassageSearcher,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Mount(mux, directory, passages)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil),
	)

	return recorder
}

func TestCachedPassageRendersDeepAnalyzerEvidenceEscaped(t *testing.T) {
	prefix := strings.Repeat("Дальний вводный материал без совпадения. ", 400)
	witness := "чрезвычайных <script>alert(1)</script> полномочий"
	document := documentstore.Document{
		NormalizedURL: "https://example.org/deep",
		Title:         "Archive",
		ExtractedText: prefix + witness,
		Language:      "ru",
	}
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Index(t.Context(), document); err != nil {
		t.Fatal(err)
	}
	local := searchlocal.NewSearcher(index)
	request := searchcore.Request{
		Query:  "чрезвычайные полномочия",
		Terms:  []string{"чрезвычайные", "полномочия"},
		Source: searchcore.SourceLocal,
		Limit:  1,
	}
	response, err := local.Search(t.Context(), request)
	if err != nil || len(response.Results) != 1 ||
		len(response.Results[0].BodyQueryMatches) != 2 {
		t.Fatalf("search response=%#v error=%v", response, err)
	}
	target := URLForLocalResult(response.Results[0], request.Terms)
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	start, err := strconv.Atoi(parsed.Query().Get(documentPassageStartParameter))
	if err != nil || start < len(prefix) {
		t.Fatalf("deep passage start=%d error=%v target=%s", start, err, target)
	}
	passages, ok := local.(searchcore.DocumentPassageSearcher)
	if !ok {
		t.Fatal("local searcher lacks passage source")
	}
	recorder := getWithDocumentPassages(
		t,
		fakeDirectory{doc: document, found: true},
		passages,
		target,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		"<mark>чрезвычайных</mark>",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"<mark>полномочий</mark>",
		"Open the full cached copy",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("passage missing %q: %s", expected, body)
		}
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("passage rendered stored markup")
	}
	assertSingleTermContextualPassage(t, local, passages, document, request)
}

func assertSingleTermContextualPassage(
	t *testing.T,
	local searchcore.Searcher,
	passages searchcore.DocumentPassageSearcher,
	document documentstore.Document,
	request searchcore.Request,
) {
	t.Helper()
	singleRequest := request
	singleRequest.Query = "полномочия"
	singleRequest.Terms = []string{"полномочия"}
	singleResponse, err := local.Search(t.Context(), singleRequest)
	if err != nil || len(singleResponse.Results) != 1 ||
		len(singleResponse.Results[0].BodyQueryMatches) != 1 {
		t.Fatalf("single-term response=%#v error=%v", singleResponse, err)
	}
	singleTarget := URLForLocalResult(singleResponse.Results[0], singleRequest.Terms)
	singleRecorder := getWithDocumentPassages(
		t,
		fakeDirectory{doc: document, found: true},
		passages,
		singleTarget,
	)
	singleBody := singleRecorder.Body.String()
	if singleRecorder.Code != http.StatusOK ||
		!strings.Contains(singleBody, "чрезвычайных &lt;script&gt;alert(1)&lt;/script&gt; ") ||
		!strings.Contains(singleBody, "<mark>полномочий</mark>") {
		t.Fatalf(
			"single-term contextual passage status=%d body=%s",
			singleRecorder.Code,
			singleBody,
		)
	}
}

func TestCachedPassageStatusMappingAndOrdinaryCopyCompatibility(t *testing.T) {
	ordinaryDirectory := fakeDirectory{found: true, doc: documentstore.Document{
		NormalizedURL: "https://example.org/plain",
		ExtractedText: "plain copy",
	}}
	ordinary := getWithDocumentPassages(
		t,
		ordinaryDirectory,
		&fakeDocumentPassageSearcher{},
		URLFor("https://example.org/plain"),
	)
	if ordinary.Code != http.StatusOK || !strings.Contains(ordinary.Body.String(), "plain copy") {
		t.Fatalf("ordinary copy status=%d body=%s", ordinary.Code, ordinary.Body.String())
	}
	target := URLForPassage(
		"https://example.org/plain",
		"en",
		[]string{"plain"},
		[]searchcore.QueryMatch{{Start: 0, End: 5}},
	)
	for name, status := range map[string]struct {
		source searchcore.DocumentPassageSearcher
		status int
	}{
		"unsupported": {source: nil, status: http.StatusNotFound},
		"missing":     {source: &fakeDocumentPassageSearcher{}, status: http.StatusNotFound},
		"failed": {
			source: &fakeDocumentPassageSearcher{err: errors.New("backend failed")},
			status: http.StatusInternalServerError,
		},
	} {
		recorder := getWithDocumentPassages(t, ordinaryDirectory, status.source, target)
		if recorder.Code != status.status {
			t.Fatalf("%s status=%d want=%d", name, recorder.Code, status.status)
		}
	}
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	document := documentstore.Document{
		NormalizedURL: "https://example.org/short",
		ExtractedText: "term",
		Language:      "en",
	}
	if err := index.Index(t.Context(), document); err != nil {
		t.Fatal(err)
	}
	local := searchlocal.NewSearcher(index).(searchcore.DocumentPassageSearcher)
	invalidRange := URLForPassage(
		document.NormalizedURL,
		"en",
		[]string{"term"},
		[]searchcore.QueryMatch{{Start: 0, End: 8}},
	)
	recorder := getWithDocumentPassages(t, ordinaryDirectory, local, invalidRange)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("stored range status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = getWithDocumentPassages(
		t,
		ordinaryDirectory,
		local,
		Path+"?u="+url.QueryEscape(document.NormalizedURL)+"&analyzer=en",
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("partial parameters status=%d", recorder.Code)
	}
}

func TestDocumentPassageLinkSelectionAndFallbacks(t *testing.T) {
	result := searchcore.Result{
		DocumentID: "https://example.org/document",
		URL:        "https://example.org/display",
		Analyzer:   "en",
		Source:     searchcore.SourceGlobal,
		BodyQueryMatches: []searchcore.QueryMatch{
			{Start: -1, End: 2},
			{Start: 15, End: 20},
			{Start: 10, End: 14},
			{Start: 10, End: 12},
			{Start: 11 << 10, End: (11 << 10) + 3},
		},
	}
	target := URLForLocalResult(result, []string{"term"})
	values, err := url.ParseQuery(strings.TrimPrefix(target, Path+"?"))
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("u") != result.DocumentID || values.Get("start") != "10" ||
		values.Get("end") != "20" || values.Get("analyzer") != "en" ||
		!strings.Contains(target, "terms=term") {
		t.Fatalf("passage URL=%s values=%v", target, values)
	}
	result.BodyQueryMatches = nil
	if got := URLForLocalResult(result, []string{"term"}); got != URLFor(result.URL) {
		t.Fatalf("plain fallback=%q", got)
	}
	result.Source = searchcore.SourceRemote
	if got := URLForLocalResult(result, []string{"term"}); got != "" {
		t.Fatalf("remote cached URL=%q", got)
	}
	if got := URLForPassage(
		"",
		"en",
		[]string{"term"},
		[]searchcore.QueryMatch{{End: 4}},
	); got != "" {
		t.Fatalf("invalid passage URL=%q", got)
	}
	if _, _, available := documentPassageLinkRange([]searchcore.QueryMatch{
		{Start: -1, End: 2},
		{Start: 2, End: 2},
		{Start: 0, End: maximumDocumentPassageAbsoluteOffset + 1},
	}); available {
		t.Fatal("invalid offsets produced a passage range")
	}
}

func TestDocumentPassageParameterValidationBounds(t *testing.T) {
	valid := url.Values{
		"u":                              {"document"},
		documentPassageAnalyzerParameter: {"cjk_zh-1"},
		documentPassageTermsParameter:    {" term "},
		documentPassageStartParameter:    {"0"},
		documentPassageEndParameter:      {"4"},
	}
	request, err := parseDocumentPassageRequest(valid, " document ")
	if err != nil || request.DocumentID != "document" || request.Analyzer != "cjk_zh-1" ||
		len(request.Terms) != 1 || request.Terms[0] != "term" ||
		request.SurroundingRunes != documentPassageSurroundingRunes {
		t.Fatalf("request=%#v error=%v", request, err)
	}
	if !passageParametersPresent(valid) || passageParametersPresent(url.Values{"u": {"document"}}) {
		t.Fatal("passage parameter detection failed")
	}
	cases := []url.Values{
		{},
		clonePassageValues(valid, "u", "document", "second"),
		clonePassageValues(valid, documentPassageAnalyzerParameter, "en", "ru"),
		clonePassageValues(valid, documentPassageAnalyzerParameter, "UPPER"),
		clonePassageValues(
			valid,
			documentPassageAnalyzerParameter,
			strings.Repeat("a", maximumDocumentPassageAnalyzerBytes+1),
		),
		clonePassageValues(valid, documentPassageTermsParameter),
		clonePassageValues(valid, documentPassageTermsParameter, ""),
		clonePassageValues(
			valid,
			documentPassageTermsParameter,
			strings.Repeat("x", maximumDocumentPassageTermBytes+1),
		),
		clonePassageValues(
			valid,
			documentPassageTermsParameter,
			strings.Repeat("x", maximumDocumentPassageTermsBytes),
			"y",
		),
		clonePassageValues(
			valid,
			documentPassageTermsParameter,
			make([]string, maximumDocumentPassageTerms+1)...),
		clonePassageValues(valid, documentPassageStartParameter, "x"),
		clonePassageValues(valid, documentPassageEndParameter, "x"),
		clonePassageValues(valid, documentPassageStartParameter, "-1"),
		clonePassageValues(valid, documentPassageStartParameter, "4"),
		clonePassageValues(
			valid,
			documentPassageEndParameter,
			strconv.Itoa(maximumDocumentPassageAbsoluteOffset+1),
		),
		clonePassageValues(
			valid,
			documentPassageEndParameter,
			strconv.Itoa(maximumDocumentPassageRangeBytes+1),
		),
	}
	for index, values := range cases {
		if _, err := parseDocumentPassageRequest(values, values.Get("u")); err == nil {
			t.Fatalf("invalid case %d accepted: %v", index, values)
		}
	}
	if validDocumentPassageAnalyzer("") || validDocumentPassageAnalyzer("é") {
		t.Fatal("invalid analyzer accepted")
	}
}

func clonePassageValues(values url.Values, key string, replacements ...string) url.Values {
	cloned := url.Values{}
	for name, entries := range values {
		cloned[name] = append([]string(nil), entries...)
	}
	cloned[key] = append([]string(nil), replacements...)

	return cloned
}
