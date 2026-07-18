package documentsearch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchlocal"
	"github.com/D4rk4/yago/yagonode/internal/searchremote"
	"github.com/D4rk4/yago/yagoproto"
)

func TestQueryMatchEvidenceSourceUsesNegotiatedStoredDocument(t *testing.T) {
	rawURL := "https://example.test/report"
	hash := hashFor("evidence")
	row := metadataRow(t, hash, rawURL, "Metadata title")
	source := queryMatchEvidenceSource{
		analyzer: testQueryMatchEvidenceAnalyzer{},
		documents: fakeDocumentDirectory{documents: map[string]documentstore.Document{
			rawURL: {
				NormalizedURL: rawURL,
				Title:         "Чрезвычайные полномочия",
				ExtractedText: strings.Repeat("введение ", 40) + "чрезвычайных полномочий",
				Language:      "ru",
			},
		}},
	}
	evidence := source.resources(t.Context(), yagoproto.SearchRequest{
		Query: []yagomodel.Hash{
			yagomodel.WordHash("чрезвычайные"),
			yagomodel.WordHash("полномочия"),
		},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"чрезвычайные", "полномочия"},
	}, []yagomodel.URIMetadataRow{row})
	item, found := evidence[hash]
	if !found || item.Version != yagoproto.QueryMatchEvidenceVersion || item.Analyzer != "ru" ||
		len(item.BodyMatches) != 2 || len(item.SnippetMatches) != 2 ||
		len(item.FieldPositions) == 0 {
		t.Fatalf("evidence = %#v", evidence)
	}
	encoded := yagoproto.SearchResponse{
		Count:            1,
		Resources:        []yagomodel.URIMetadataRow{row},
		ResourceEvidence: evidence,
	}.Encode()
	parsed, err := yagoproto.ParseSearchResponse(encoded)
	if err != nil || len(parsed.ResourceEvidence) != 1 {
		t.Fatalf("wire evidence=%#v err=%v", parsed.ResourceEvidence, err)
	}
}

func TestQueryMatchEvidenceSourceKeepsLegacyAndUnavailableRowsUnchanged(t *testing.T) {
	rawURL := "https://example.test/report"
	hash := hashFor("evidence")
	row := metadataRow(t, hash, rawURL, "Metadata title")
	source := queryMatchEvidenceSource{
		analyzer:  testQueryMatchEvidenceAnalyzer{},
		documents: fakeDocumentDirectory{documents: map[string]documentstore.Document{}},
	}
	requests := []yagoproto.SearchRequest{
		{},
		{EvidenceVersion: 2, EvidenceTerms: []string{"term"}},
		{EvidenceVersion: yagoproto.QueryMatchEvidenceVersion},
		{EvidenceVersion: yagoproto.QueryMatchEvidenceVersion, EvidenceTerms: []string{"term"}},
	}
	for index, request := range requests {
		if got := source.resources(
			t.Context(),
			request,
			[]yagomodel.URIMetadataRow{row},
		); got != nil {
			t.Fatalf("case %d evidence = %#v", index, got)
		}
	}
	source.documents = fakeDocumentDirectory{err: context.Canceled}
	if got := source.resources(
		t.Context(),
		requests[3],
		[]yagomodel.URIMetadataRow{row},
	); got != nil {
		t.Fatalf("read failure evidence = %#v", got)
	}
	malformed := yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash: hash.String(),
		yagomodel.URLMetaURL:  "%%%",
	}}
	if got := source.resources(
		t.Context(),
		requests[3],
		[]yagomodel.URIMetadataRow{malformed},
	); got != nil {
		t.Fatalf("malformed row evidence = %#v", got)
	}
}

func TestQueryMatchEvidenceSourceRequiresPrimaryHashesOrURLBound(t *testing.T) {
	rawURL := "https://example.test/report"
	hash := hashFor("evidence")
	row := metadataRow(t, hash, rawURL, "Term")
	source := queryMatchEvidenceSource{
		analyzer: testQueryMatchEvidenceAnalyzer{},
		documents: fakeDocumentDirectory{documents: map[string]documentstore.Document{
			rawURL: {
				NormalizedURL: rawURL,
				Title:         "Term",
				ExtractedText: "term",
				Language:      "en",
			},
		}},
	}
	request := yagoproto.SearchRequest{
		Query:           []yagomodel.Hash{yagomodel.WordHash("other")},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"term"},
	}
	if evidence := source.resources(
		t.Context(),
		request,
		[]yagomodel.URIMetadataRow{row},
	); evidence != nil {
		t.Fatalf("mismatched primary evidence = %#v", evidence)
	}
	request.Query = nil
	request.URLs = []yagomodel.Hash{hash}
	if evidence := source.resources(
		t.Context(),
		request,
		[]yagomodel.URIMetadataRow{row},
	); len(
		evidence,
	) != 1 {
		t.Fatalf("URL-bounded secondary evidence = %#v", evidence)
	}
	disallowedHash := hashFor("disallowed")
	disallowedURL := "https://example.test/disallowed"
	source.documents = fakeDocumentDirectory{documents: map[string]documentstore.Document{
		disallowedURL: {
			NormalizedURL: disallowedURL,
			Title:         "Term",
			ExtractedText: "term",
			Language:      "en",
		},
	}}
	disallowed := metadataRow(t, disallowedHash, disallowedURL, "Term")
	if evidence := source.resources(
		t.Context(),
		request,
		[]yagomodel.URIMetadataRow{disallowed},
	); evidence != nil {
		t.Fatalf("out-of-allowlist evidence = %#v", evidence)
	}
	malformed := yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash: "bad",
	}}
	if evidence := source.resources(
		t.Context(),
		request,
		[]yagomodel.URIMetadataRow{malformed},
	); evidence != nil {
		t.Fatalf("malformed allowlist evidence = %#v", evidence)
	}
}

func TestQueryMatchEvidenceSourceRejectsInvalidResourceAndAnalysisInput(t *testing.T) {
	rawURL := "https://example.test/report"
	hash := hashFor("evidence")
	source := queryMatchEvidenceSource{
		analyzer: testQueryMatchEvidenceAnalyzer{},
		documents: fakeDocumentDirectory{documents: map[string]documentstore.Document{
			rawURL: {
				NormalizedURL: rawURL,
				Title:         "term",
				ExtractedText: "term",
			},
		}},
	}
	invalidRows := []yagomodel.URIMetadataRow{
		{Properties: map[string]string{
			yagomodel.URLMetaHash: "bad",
			yagomodel.URLMetaURL:  yagomodel.EncodeBase64WireForm(rawURL),
		}},
		{Properties: map[string]string{
			yagomodel.URLMetaHash: hash.String(),
			yagomodel.URLMetaURL:  "",
		}},
	}
	for index, row := range invalidRows {
		_, _, _, available := source.resource(t.Context(), []string{"term"}, row, 1024)
		if available {
			t.Fatalf("case %d evidence available", index)
		}
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, _, available := source.resource(
		canceled,
		[]string{"term"},
		metadataRow(t, hash, rawURL, "term"),
		1024,
	)
	if available {
		t.Fatal("canceled analysis evidence available")
	}

	source.documents = fakeDocumentDirectory{documents: map[string]documentstore.Document{
		rawURL: {Title: "bad\xff"},
	}}
	_, _, _, available = source.resource(
		t.Context(),
		[]string{"term"},
		metadataRow(t, hash, rawURL, "term"),
		1024,
	)
	if available {
		t.Fatal("invalid document evidence available")
	}

	fields := ProtocolQueryFieldPositions(map[string]map[int][]int{
		"title": {0: nil},
		"body":  {0: {1}},
	})
	if len(fields) != 1 || fields[0].Field != "body" {
		t.Fatalf("filtered fields = %#v", fields)
	}
}

func TestQueryMatchEvidenceSourceBoundsCandidatesAnalysisAndPositions(t *testing.T) {
	documents := make(map[string]documentstore.Document)
	rows := make([]yagomodel.URIMetadataRow, maximumInboundEvidenceCandidates+4)
	for index := range rows {
		rawURL := fmt.Sprintf("https://example.test/%d", index)
		hash := hashFor(fmt.Sprintf("%012d", index))
		rows[index] = metadataRow(t, hash, rawURL, "term")
		documents[rawURL] = documentstore.Document{
			NormalizedURL: rawURL,
			Title:         "term",
			ExtractedText: "term",
			Language:      "en",
		}
	}
	source := queryMatchEvidenceSource{
		analyzer:  testQueryMatchEvidenceAnalyzer{},
		documents: fakeDocumentDirectory{documents: documents},
	}
	evidence := source.resources(t.Context(), yagoproto.SearchRequest{
		Query:           []yagomodel.Hash{yagomodel.WordHash("term")},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"term"},
	}, rows)
	if len(evidence) != maximumInboundEvidenceCandidates {
		t.Fatalf("candidate evidence = %d", len(evidence))
	}
	fields := map[string]map[int][]int{
		"title": {0: sequentialPositions()},
		"body": {
			0: sequentialPositions(),
			1: sequentialPositions(),
			2: sequentialPositions(),
		},
	}
	positionTotal := 0
	for _, field := range ProtocolQueryFieldPositions(fields) {
		for _, requirement := range field.Requirements {
			positionTotal += len(requirement.Positions)
		}
	}
	if positionTotal != MaximumQueryMatchEvidencePositions {
		t.Fatalf("positions = %d", positionTotal)
	}
}

func TestQueryMatchEvidenceSourceEnforcesSharedByteAndTimeBudgets(t *testing.T) {
	firstURL := "https://example.test/first"
	secondURL := "https://example.test/second"
	firstHash := hashFor("first")
	secondHash := hashFor("second")
	rows := []yagomodel.URIMetadataRow{
		metadataRow(t, firstHash, firstURL, "term"),
		metadataRow(t, secondHash, secondURL, "term"),
	}
	documents := fakeDocumentDirectory{documents: map[string]documentstore.Document{
		firstURL:  {Title: "term", ExtractedText: "term"},
		secondURL: {Title: "term", ExtractedText: "term"},
	}}
	request := yagoproto.SearchRequest{
		Query:           []yagomodel.Hash{yagomodel.WordHash("term")},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"term"},
	}
	analysisBounded := queryMatchEvidenceSource{
		documents: documents,
		analyzer:  testQueryMatchEvidenceAnalyzer{},
		budget: queryMatchEvidenceBudget{
			candidates:    2,
			analysisBytes: len("term"),
			responseBytes: maximumInboundEvidenceResponseBytes,
			duration:      time.Second,
		},
	}.resources(t.Context(), request, rows)
	if len(analysisBounded) != 1 {
		t.Fatalf("analysis-bounded evidence = %#v", analysisBounded)
	}
	responseBounded := queryMatchEvidenceSource{
		documents: documents,
		analyzer:  testQueryMatchEvidenceAnalyzer{},
		budget: queryMatchEvidenceBudget{
			candidates:    2,
			analysisBytes: 1024,
			responseBytes: 1,
			duration:      time.Second,
		},
	}.resources(t.Context(), request, rows)
	if responseBounded != nil {
		t.Fatalf("response-bounded evidence = %#v", responseBounded)
	}
	item, _, _, available := queryMatchEvidenceSource{
		documents: documents,
		analyzer:  testQueryMatchEvidenceAnalyzer{},
	}.resource(
		t.Context(),
		request.EvidenceTerms,
		rows[0],
		1024,
	)
	if !available {
		t.Fatal("fixture evidence unavailable")
	}
	rawEvidence, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal fixture evidence: %v", err)
	}
	encodedEvidenceBytes := base64.RawURLEncoding.EncodedLen(len(rawEvidence))
	if encodedEvidenceBytes <= len(rawEvidence) {
		t.Fatalf("encoded evidence bytes = %d, raw = %d", encodedEvidenceBytes, len(rawEvidence))
	}
	base64Bounded := queryMatchEvidenceSource{
		documents: documents,
		analyzer:  testQueryMatchEvidenceAnalyzer{},
		budget: queryMatchEvidenceBudget{
			candidates:    1,
			analysisBytes: 1024,
			responseBytes: len(rawEvidence),
			duration:      time.Second,
		},
	}.resources(t.Context(), request, rows[:1])
	if base64Bounded != nil {
		t.Fatalf("base64-bounded evidence = %#v", base64Bounded)
	}
	assertQueryEvidenceTimeBudget(t, request, rows[:1])
}

func assertQueryEvidenceTimeBudget(
	t *testing.T,
	request yagoproto.SearchRequest,
	rows []yagomodel.URIMetadataRow,
) {
	t.Helper()
	started := time.Now()
	timeBounded := queryMatchEvidenceSource{
		documents: blockingEvidenceDocumentDirectory{},
		analyzer:  testQueryMatchEvidenceAnalyzer{},
		budget: queryMatchEvidenceBudget{
			candidates:    1,
			analysisBytes: 1024,
			responseBytes: 1024,
			duration:      5 * time.Millisecond,
		},
	}.resources(t.Context(), request, rows)
	if timeBounded != nil || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("time-bounded evidence=%#v elapsed=%s", timeBounded, time.Since(started))
	}
}

func TestNegotiatedSearchEndpointPublishesEvidenceOnlyForVersionOne(t *testing.T) {
	firstWord := yagomodel.WordHash("чрезвычайные")
	secondWord := yagomodel.WordHash("полномочия")
	documentHash := hashFor("document")
	rawURL := "https://example.test/report"
	row := metadataRow(t, documentHash, rawURL, "Metadata title")
	mux := http.NewServeMux()
	MountSearch(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(searchWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		searchIdentity(),
		SearchConfig{
			Index: fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
				firstWord:  {postingEntry(firstWord, documentHash.String(), 1, 1)},
				secondWord: {postingEntry(secondWord, documentHash.String(), 2, 1)},
			}},
			Documents: fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
				documentHash: row,
			}},
			DocumentStore: fakeDocumentDirectory{documents: map[string]documentstore.Document{
				rawURL: {
					NormalizedURL: rawURL,
					Title:         "Чрезвычайные полномочия",
					ExtractedText: "чрезвычайных полномочий",
					Language:      "ru",
				},
			}},
			Evidence:       testQueryMatchEvidenceAnalyzer{},
			MatchesPerTerm: 10,
		},
	)

	request := yagoproto.SearchRequest{
		NetworkName:     "freeworld",
		Query:           []yagomodel.Hash{firstWord, secondWord},
		Count:           1,
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"чрезвычайные", "полномочия"},
	}
	parsed := requestSearchResponse(t, mux, request)
	if len(parsed.ResourceEvidence) != 1 || parsed.ResourceEvidence[documentHash].Analyzer != "ru" {
		t.Fatalf("negotiated evidence = %#v", parsed.ResourceEvidence)
	}
	request.EvidenceVersion = 0
	request.EvidenceTerms = nil
	parsed = requestSearchResponse(t, mux, request)
	if parsed.ResourceEvidence != nil {
		t.Fatalf("legacy evidence = %#v", parsed.ResourceEvidence)
	}
}

func TestQueryMatchEvidenceFlowsBetweenTwoNodeSearchSurfaces(t *testing.T) {
	requirements := []string{"чрезвычайные", "полномочия"}
	firstWord := yagomodel.WordHash(requirements[0])
	secondWord := yagomodel.WordHash(requirements[1])
	documentHash := hashFor("document")
	rawURL := "https://example.test/report"
	row := metadataRow(t, documentHash, rawURL, "Metadata title")
	mux := http.NewServeMux()
	MountSearch(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(searchWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		searchIdentity(),
		SearchConfig{
			Index: fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
				firstWord:  {postingEntry(firstWord, documentHash.String(), 12, 1)},
				secondWord: {postingEntry(secondWord, documentHash.String(), 13, 1)},
			}},
			Documents: fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
				documentHash: row,
			}},
			DocumentStore: fakeDocumentDirectory{documents: map[string]documentstore.Document{
				rawURL: {
					NormalizedURL: rawURL,
					Title:         "Чрезвычайные полномочия",
					ExtractedText: strings.Repeat("введение ", 40) + "чрезвычайных полномочий",
					Language:      "ru",
				},
			}},
			Evidence:       testQueryMatchEvidenceAnalyzer{},
			MatchesPerTerm: 10,
		},
	)
	server := httptest.NewServer(mux)
	defer server.Close()
	response, err := searchremote.NewSearcher(searchremote.Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       queryEvidencePeerSource{peer: queryEvidencePeer(t, server.URL)},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: 1,
	}).Search(t.Context(), searchcore.Request{
		Terms:  requirements,
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("remote Search: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("response = %#v", response)
	}
	result := response.Results[0]
	if !result.EvidenceReady || result.Analyzer != "ru" ||
		len(result.QueryMatches) != 2 || len(result.BodyQueryMatches) != 2 ||
		len(result.FieldTermPositions["body"][requirements[0]]) == 0 ||
		len(result.FieldTermPositions["body"][requirements[1]]) == 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestNegotiatedAnalyzerRecallFindsRemoteOnlySiblingAcrossTwoNodes(t *testing.T) {
	requirements := []string{"чрезвычайные", "полномочия"}
	rawURL := "https://example.test/remote-inflection"
	document := documentstore.Document{
		NormalizedURL: rawURL,
		Title:         "Чрезвычайные полномочия",
		ExtractedText: strings.Repeat("введение ", 40) + "чрезвычайных полномочий",
		Language:      "ru",
	}
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Index(t.Context(), document); err != nil {
		t.Fatal(err)
	}
	documentHash, _ := yagomodel.HashURL(rawURL)
	row := metadataRow(t, documentHash.Hash(), rawURL, document.Title)
	mux := http.NewServeMux()
	MountSearch(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(searchWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		searchIdentity(),
		SearchConfig{
			Index: fakeScanner{},
			Documents: fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
				documentHash.Hash(): row,
			}},
			DocumentStore: fakeDocumentDirectory{documents: map[string]documentstore.Document{
				rawURL: document,
			}},
			AnalyzerSearch: searchlocal.NewSearcher(index),
			Evidence:       testQueryMatchEvidenceAnalyzer{},
			MatchesPerTerm: 10,
		},
	)
	server := httptest.NewServer(mux)
	defer server.Close()
	response, err := searchremote.NewSearcher(searchremote.Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       queryEvidencePeerSource{peer: queryEvidencePeer(t, server.URL)},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: 1,
	}).Search(t.Context(), searchcore.Request{
		Query:  strings.Join(requirements, " "),
		Terms:  requirements,
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].URL != rawURL ||
		!response.Results[0].EvidenceReady || response.Results[0].Analyzer != "ru" ||
		len(response.Results[0].BodyQueryMatches) != 2 {
		t.Fatalf("analyzer-backed remote response = %#v", response)
	}
}

func TestMorphologyVariantEvidenceFlowsFromSelectedSiblingPeer(t *testing.T) {
	original := "полномочия"
	sibling := "полномочий"
	rawURL := "https://example.test/selected-sibling"
	document := documentstore.Document{
		NormalizedURL: rawURL,
		Title:         "Полномочий достаточно",
		ExtractedText: strings.Repeat("введение ", 40) + "полномочий достаточно",
		Language:      "ru",
	}
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Index(t.Context(), document); err != nil {
		t.Fatal(err)
	}
	documentHash, _ := yagomodel.HashURL(rawURL)
	row := metadataRow(t, documentHash.Hash(), rawURL, document.Title)
	fixture := newSelectedSiblingPeerFixture(t, selectedSiblingPeerDocument{
		original: original,
		sibling:  sibling,
		rawURL:   rawURL,
		hash:     documentHash.Hash(),
		document: document,
		index:    index,
		row:      row,
	})
	response := fixture.search(t)
	if len(response.Results) != 1 {
		t.Fatalf("response = %#v", response)
	}
	result := response.Results[0]
	if result.URL != rawURL || !result.EvidenceReady || result.Analyzer != "ru" ||
		len(result.BodyQueryMatches) != 1 ||
		len(result.FieldTermPositions["body"][original]) == 0 ||
		len(result.FieldTermPositions["body"][sibling]) != 0 {
		t.Fatalf("variant evidence result = %#v", result)
	}
}

type selectedSiblingPeerDocument struct {
	original string
	sibling  string
	rawURL   string
	hash     yagomodel.Hash
	document documentstore.Document
	index    searchindex.SearchIndex
	row      yagomodel.URIMetadataRow
}

type selectedSiblingPeerFixture struct {
	document          selectedSiblingPeerDocument
	realServer        *httptest.Server
	decoyServer       *httptest.Server
	realRequirements  chan string
	decoyRequirements chan string
}

func newSelectedSiblingPeerFixture(
	t *testing.T,
	document selectedSiblingPeerDocument,
) selectedSiblingPeerFixture {
	t.Helper()
	realMux := http.NewServeMux()
	MountSearch(
		httpguard.NewWireRouter(realMux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(searchWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		searchIdentity(),
		SearchConfig{
			Index: fakeScanner{},
			Documents: fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
				document.hash: document.row,
			}},
			DocumentStore: fakeDocumentDirectory{documents: map[string]documentstore.Document{
				document.rawURL: document.document,
			}},
			AnalyzerSearch: searchlocal.NewSearcher(document.index),
			Evidence:       testQueryMatchEvidenceAnalyzer{},
			MatchesPerTerm: 10,
		},
	)
	realRequirements := make(chan string, 4)
	realServer := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		request *http.Request,
	) {
		realRequirements <- request.URL.Query().Get(yagoproto.FieldQueryEvidenceTerm)
		realMux.ServeHTTP(w, request)
	}))
	t.Cleanup(realServer.Close)
	decoyRequirements := make(chan string, 4)
	decoyServer := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		request *http.Request,
	) {
		decoyRequirements <- request.URL.Query().Get(yagoproto.FieldQueryEvidenceTerm)
		http.ServeContent(
			w,
			request,
			"",
			time.Time{},
			strings.NewReader(yagoproto.SearchResponse{}.Encode().Encode()),
		)
	}))
	t.Cleanup(decoyServer.Close)

	return selectedSiblingPeerFixture{
		document:          document,
		realServer:        realServer,
		decoyServer:       decoyServer,
		realRequirements:  realRequirements,
		decoyRequirements: decoyRequirements,
	}
}

func (fixture selectedSiblingPeerFixture) search(t *testing.T) searchcore.Response {
	t.Helper()
	realPeer := queryEvidencePeer(t, fixture.realServer.URL)
	realPeer.Hash = yagomodel.WordHash(fixture.document.sibling)
	decoyPeer := queryEvidencePeer(t, fixture.decoyServer.URL)
	decoyPeer.Hash = yagomodel.WordHash(fixture.document.original)
	response, err := searchremote.NewSearcher(searchremote.Config{
		Client:             fixture.realServer.Client(),
		NetworkName:        "freeworld",
		Peers:              queryEvidencePeerSet{decoyPeer, realPeer},
		MaxPeers:           1,
		Redundancy:         1,
		MinimumPeerAgeDays: -1,
		MinimumPeerRWIs:    -1,
		Concurrency:        1,
		ExpandWord: func(string) []string {
			return []string{fixture.document.sibling}
		},
	}).Search(t.Context(), searchcore.Request{
		Query:  fixture.document.original,
		Terms:  []string{fixture.document.original},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	gotDecoy := receivedEvidenceRequirement(t, fixture.decoyRequirements)
	if gotDecoy != fixture.document.original {
		t.Fatalf("original peer evidence requirement = %q", gotDecoy)
	}
	gotReal := receivedEvidenceRequirement(t, fixture.realRequirements)
	if gotReal != fixture.document.sibling {
		t.Fatalf("sibling peer evidence requirement = %q", gotReal)
	}
	if len(fixture.decoyRequirements) != 0 || len(fixture.realRequirements) != 0 {
		t.Fatalf(
			"unexpected peer requests: original=%d sibling=%d",
			len(fixture.decoyRequirements),
			len(fixture.realRequirements),
		)
	}

	return response
}

type queryEvidencePeerSource struct {
	peer yagomodel.Seed
}

type queryEvidencePeerSet []yagomodel.Seed

type blockingEvidenceDocumentDirectory struct{}

func (blockingEvidenceDocumentDirectory) Document(
	ctx context.Context,
	_ string,
) (documentstore.Document, bool, error) {
	<-ctx.Done()

	return documentstore.Document{}, false, fmt.Errorf("blocking evidence directory: %w", ctx.Err())
}

func (blockingEvidenceDocumentDirectory) Count(context.Context) (int, error) {
	return 0, nil
}

func (s queryEvidencePeerSource) SearchTargetPeers(context.Context) []yagomodel.Seed {
	return []yagomodel.Seed{s.peer}
}

func (peers queryEvidencePeerSet) SearchTargetPeers(context.Context) []yagomodel.Seed {
	return append([]yagomodel.Seed(nil), peers...)
}

func receivedEvidenceRequirement(t *testing.T, received <-chan string) string {
	t.Helper()
	select {
	case requirement := <-received:
		return requirement
	case <-time.After(time.Second):
		t.Fatal("peer request was not received")
		return ""
	}
}

func queryEvidencePeer(t *testing.T, rawURL string) yagomodel.Seed {
	t.Helper()
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse peer URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split peer address: %v", err)
	}
	ip, err := yagomodel.ParseHost(host)
	if err != nil {
		t.Fatalf("parse peer host: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse peer port: %v", err)
	}

	return yagomodel.Seed{
		Hash:     hashFor("peer"),
		IP:       yagomodel.Some(ip),
		Port:     yagomodel.Some(yagomodel.Port(port)),
		Flags:    yagomodel.Some(yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true)),
		RWICount: yagomodel.Some(1),
	}
}

func sequentialPositions() []int {
	positions := make([]int, 100)
	for index := range positions {
		positions[index] = index + 1
	}

	return positions
}

func requestSearchResponse(
	t *testing.T,
	mux *http.ServeMux,
	request yagoproto.SearchRequest,
) yagoproto.SearchResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathSearch+"?"+request.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(recorder, httpRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	parsed, err := yagoproto.ParseSearchResponse(mustParseMessage(t, recorder.Body.String()))
	if err != nil {
		t.Fatalf("ParseSearchResponse: %v", err)
	}

	return parsed
}

type testQueryMatchEvidenceAnalyzer struct{}

func (testQueryMatchEvidenceAnalyzer) AnalyzeQueryMatchEvidence(
	ctx context.Context,
	document documentstore.Document,
	requirements []string,
	byteLimit int,
) (yagoproto.QueryMatchEvidence, int, bool, error) {
	evidence, analyzedBytes, available, err := searchindex.AnalyzeDocumentQueryEvidence(
		ctx,
		document,
		requirements,
		byteLimit,
	)
	if err != nil {
		return yagoproto.QueryMatchEvidence{}, analyzedBytes, false,
			fmt.Errorf("analyze test query match evidence: %w", err)
	}
	if !available {
		return yagoproto.QueryMatchEvidence{}, analyzedBytes, false, nil
	}

	return yagoproto.QueryMatchEvidence{
		Version:             yagoproto.QueryMatchEvidenceVersion,
		Analyzer:            evidence.Analyzer,
		RequirementOrdinals: evidence.RequirementOrdinals,
		AbsentOrdinals:      evidence.AbsentOrdinals,
		Snippet:             evidence.Snippet,
		SnippetMatches:      testProtocolQueryMatchRanges(evidence.SnippetMatches),
		BodyMatches:         testProtocolQueryMatchRanges(evidence.BodyMatches),
		FieldPositions: ProtocolQueryFieldPositions(
			evidence.FieldRequirementPositions,
		),
	}, analyzedBytes, true, nil
}

func testProtocolQueryMatchRanges(
	matches []searchindex.TextQueryMatch,
) []yagoproto.QueryMatchRange {
	mapped := make([]yagoproto.QueryMatchRange, len(matches))
	for index, match := range matches {
		mapped[index] = yagoproto.QueryMatchRange{Start: match.Start, End: match.End}
	}

	return mapped
}
