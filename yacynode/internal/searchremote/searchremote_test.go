package searchremote

import (
	"context"
	"errors"
	"html/template"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/searchcore"
	"github.com/D4rk4/yago/yacyproto"
)

var fixtureResponseTemplate = template.Must(template.New("fixture").Parse("{{.}}"))

type fakePeerSource struct {
	peers []yacymodel.Seed
}

func (s fakePeerSource) ReachablePeers(context.Context) []yacymodel.Seed {
	return s.peers
}

func TestRemoteSearcherQueriesPeersAndNormalizesResults(t *testing.T) {
	word := yacymodel.WordHash("golang")
	hash := hashFor("doc1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != yacyproto.PathSearch ||
			r.URL.Query().Get(yacyproto.FieldQuery) != word.String() ||
			r.URL.Query().Get(yacyproto.FieldNetworkName) != "freeworld" {
			t.Fatalf("request path/query = %s %s", r.URL.Path, r.URL.RawQuery)
		}
		response := yacyproto.SearchResponse{
			JoinCount: 1,
			Count:     1,
			Resources: []yacymodel.URIMetadataRow{
				metadataRow(t, hash, "https://example.org/doc.html", "Remote Result"),
			},
		}
		message := response.Encode()
		yacyproto.InjectResponseHeader(message, "1.940", 42)
		writeFixtureResponse(t, w, message.Encode())
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yacymodel.Seed{serverSeed(t, server.URL)}},
	}).Search(t.Context(), searchcore.Request{
		Terms:         []string{"golang"},
		Source:        searchcore.SourceGlobal,
		Limit:         10,
		ContentDomain: searchcore.ContentDomainText,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.TotalResults != 1 || len(resp.Results) != 1 || len(resp.PartialFailures) != 0 {
		t.Fatalf("response = %#v", resp)
	}
	result := resp.Results[0]
	if result.Title != "Remote Result" ||
		result.URL != "https://example.org/doc.html" ||
		result.Source != searchcore.SourceRemote ||
		result.URLHash != hash.String() ||
		result.Score != 0.5 {
		t.Fatalf("result = %#v", result)
	}
}

func TestRemoteSearcherSelectsDHTTargetByWordHash(t *testing.T) {
	word := yacymodel.WordHash("golang")
	unused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("peer outside the DHT target set should not be queried")
	}))
	defer unused.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get(yacyproto.FieldQuery) != word.String() {
			t.Fatalf("query = %q, want %q", r.URL.Query().Get(yacyproto.FieldQuery), word)
		}
		writeFixtureResponse(t, w, yacyproto.SearchResponse{}.Encode().Encode())
	}))
	defer target.Close()

	resp, err := NewSearcher(Config{
		Client:      target.Client(),
		NetworkName: "freeworld",
		Peers: fakePeerSource{peers: []yacymodel.Seed{
			serverSeedWithHash(t, unused.URL, hashFor("ZZZZZZZZZZZZ")),
			serverSeedWithHash(t, target.URL, word),
		}},
		MaxPeers:   1,
		Redundancy: 1,
	}).Search(t.Context(), searchcore.Request{
		Terms:  []string{"golang"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.PartialFailures) != 0 {
		t.Fatalf("partial failures = %#v", resp.PartialFailures)
	}
}

func TestRemoteSearcherUsesIndexAbstractsForMultiTermSearch(t *testing.T) {
	fixture := newMultiTermAbstractFixture(t)
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yacymodel.Seed{serverSeed(t, server.URL)}},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: 1,
	}).Search(t.Context(), searchcore.Request{
		Terms:  []string{"alpha", "beta"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.TotalResults != 2 ||
		len(resp.Results) != 1 ||
		resp.Results[0].URLHash != fixture.shared.String() ||
		len(resp.PartialFailures) != 0 {
		t.Fatalf("response = %#v", resp)
	}
	if requests := fixture.recordedRequests(); len(requests) != 4 {
		t.Fatalf("request count = %d, want 4; requests=%v", len(requests), requests)
	}
}

type multiTermAbstractFixture struct {
	tb        testing.TB
	alpha     yacymodel.Hash
	beta      yacymodel.Hash
	shared    yacymodel.Hash
	alphaOnly yacymodel.Hash
	betaOnly  yacymodel.Hash
	requests  []url.Values
	mu        sync.Mutex
}

func newMultiTermAbstractFixture(tb testing.TB) *multiTermAbstractFixture {
	tb.Helper()

	return &multiTermAbstractFixture{
		tb:        tb,
		alpha:     yacymodel.WordHash("alpha"),
		beta:      yacymodel.WordHash("beta"),
		shared:    hashFor("shared"),
		alphaOnly: hashFor("alpha-only"),
		betaOnly:  hashFor("beta-only"),
	}
}

func (f *multiTermAbstractFixture) serve(w http.ResponseWriter, r *http.Request) {
	form := r.URL.Query()
	f.record(form)

	switch {
	case form.Get(yacyproto.FieldQuery) == "" &&
		form.Get(yacyproto.FieldAbstracts) == f.alpha.String():
		f.writeAbstract(w, f.alpha, f.alphaOnly)
	case form.Get(yacyproto.FieldQuery) == "" &&
		form.Get(yacyproto.FieldAbstracts) == f.beta.String():
		f.writeAbstract(w, f.beta, f.betaOnly)
	case form.Get(yacyproto.FieldQuery) == f.alpha.String() &&
		form.Get(yacyproto.FieldURLs) == f.shared.String():
		f.writeSecondary(w)
	case form.Get(yacyproto.FieldQuery) == f.beta.String() &&
		form.Get(yacyproto.FieldURLs) == f.shared.String():
		f.writeSecondary(w)
	default:
		f.tb.Fatalf("unexpected remote search request: %s", r.URL.RawQuery)
	}
}

func (f *multiTermAbstractFixture) record(form url.Values) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.requests = append(f.requests, form)
}

func (f *multiTermAbstractFixture) recordedRequests() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]url.Values(nil), f.requests...)
}

func (f *multiTermAbstractFixture) writeAbstract(
	w http.ResponseWriter,
	term yacymodel.Hash,
	termOnly yacymodel.Hash,
) {
	writeFixtureResponse(f.tb, w, yacyproto.SearchResponse{
		IndexCount: map[yacymodel.Hash]int{term: 2},
		IndexAbstract: map[yacymodel.Hash]string{
			term: yacymodel.EncodeSearchIndexAbstract([]yacymodel.Hash{
				termOnly,
				f.shared,
			}),
		},
	}.Encode().Encode())
}

func (f *multiTermAbstractFixture) writeSecondary(w http.ResponseWriter) {
	writeFixtureResponse(f.tb, w, yacyproto.SearchResponse{
		JoinCount: 1,
		Count:     1,
		Resources: []yacymodel.URIMetadataRow{
			metadataRow(f.tb, f.shared, "https://example.org/shared", "Shared"),
		},
	}.Encode().Encode())
}

func TestRemoteSearcherReportsNoPeers(t *testing.T) {
	resp, err := NewSearcher(Config{}).Search(
		t.Context(),
		searchcore.Request{Source: searchcore.SourceGlobal, Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.PartialFailures) != 1 || resp.PartialFailures[0].Reason != "no reachable peers" {
		t.Fatalf("response = %#v", resp)
	}

	resp, err = NewSearcher(Config{Peers: fakePeerSource{}}).Search(
		t.Context(),
		searchcore.Request{Terms: []string{"golang"}, Source: searchcore.SourceGlobal, Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search with empty roster: %v", err)
	}
	if len(resp.PartialFailures) != 1 || resp.PartialFailures[0].Reason != "no reachable peers" {
		t.Fatalf("empty roster response = %#v", resp)
	}
}

func TestRemoteSearcherReportsNoQueryTerms(t *testing.T) {
	unused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("empty query should not trigger remote search")
	}))
	defer unused.Close()

	resp, err := NewSearcher(Config{
		Client: unused.Client(),
		Peers:  fakePeerSource{peers: []yacymodel.Seed{serverSeed(t, unused.URL)}},
	}).Search(t.Context(), searchcore.Request{Source: searchcore.SourceGlobal, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.PartialFailures) != 1 || resp.PartialFailures[0].Reason != "no query terms" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherReportsNoTargetsForMultiTermSearch(t *testing.T) {
	resp, err := NewSearcher(Config{}).Search(
		t.Context(),
		searchcore.Request{Terms: []string{"golang", "yacy"}, Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.PartialFailures) != 2 ||
		!strings.Contains(resp.PartialFailures[0].Reason, "no dht search targets") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherReportsNoDHTTargets(t *testing.T) {
	unused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("ineligible peer should not be queried")
	}))
	defer unused.Close()
	peer := serverSeed(t, unused.URL)
	peer.Flags = yacymodel.Some(yacymodel.ZeroFlags())

	resp, err := NewSearcher(Config{
		Client: unused.Client(),
		Peers:  fakePeerSource{peers: []yacymodel.Seed{peer}},
	}).Search(t.Context(), searchcore.Request{
		Terms:  []string{"golang"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.PartialFailures) != 1 || resp.PartialFailures[0].Reason != "no dht search targets" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherReportsMalformedIndexAbstract(t *testing.T) {
	alpha := yacymodel.WordHash("alpha")
	beta := yacymodel.WordHash("beta")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get(yacyproto.FieldAbstracts) {
		case alpha.String():
			writeFixtureResponse(t, w, yacyproto.SearchResponse{
				IndexAbstract: map[yacymodel.Hash]string{alpha: "{bad"},
			}.Encode().Encode())
		case beta.String():
			writeFixtureResponse(t, w, yacyproto.SearchResponse{
				IndexAbstract: map[yacymodel.Hash]string{
					beta: yacymodel.EncodeSearchIndexAbstract([]yacymodel.Hash{hashFor("beta")}),
				},
			}.Encode().Encode())
		default:
			t.Fatalf("unexpected request: %s", r.URL.RawQuery)
		}
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		Peers:       fakePeerSource{peers: []yacymodel.Seed{serverSeed(t, server.URL)}},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: 1,
	}).Search(t.Context(), searchcore.Request{
		Terms: []string{"alpha", "beta"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 0 ||
		len(resp.PartialFailures) != 1 ||
		!strings.Contains(resp.PartialFailures[0].Reason, "index abstract") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherReportsIndexAbstractPeerFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		Peers:       fakePeerSource{peers: []yacymodel.Seed{serverSeed(t, server.URL)}},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: 1,
	}).Search(t.Context(), searchcore.Request{
		Terms: []string{"alpha", "beta"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 0 ||
		len(resp.PartialFailures) != 4 ||
		!strings.Contains(resp.PartialFailures[0].Reason, "status 502") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherReportsMissingIndexAbstractResponses(t *testing.T) {
	term := yacymodel.WordHash("alpha")
	abstracts, failures := (searcher{}).termAbstracts(
		t.Context(),
		searchcore.Request{},
		[]termPeerTargets{{term: term}},
	)
	if len(abstracts) != 0 ||
		len(failures) != 1 ||
		!strings.Contains(failures[0].Reason, "no index abstract responses") {
		t.Fatalf("abstracts=%#v failures=%#v", abstracts, failures)
	}
}

func TestRemoteSearcherSkipsPeersWithoutRWIInventory(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("empty RWI peer should not be queried")
	}))
	defer empty.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureResponse(t, w, yacyproto.SearchResponse{}.Encode().Encode())
	}))
	defer target.Close()

	emptySeed := serverSeed(t, empty.URL)
	emptySeed.RWICount = yacymodel.Some(0)

	resp, err := NewSearcher(Config{
		Client: target.Client(),
		Peers: fakePeerSource{peers: []yacymodel.Seed{
			emptySeed,
			serverSeedWithHash(t, target.URL, yacymodel.WordHash("golang")),
		}},
		MaxPeers:   1,
		Redundancy: 1,
	}).Search(t.Context(), searchcore.Request{
		Terms: []string{"golang"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.PartialFailures) != 0 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherReportsDHTSelectionFailure(t *testing.T) {
	resp, err := NewSearcher(Config{
		Peers: fakePeerSource{peers: []yacymodel.Seed{
			searchSeed(t, "AAAAAAAAAAAA"),
			searchSeed(t, "BBBBBBBBBBBB"),
		}},
		MaxPeers:   2,
		Redundancy: 1,
		RandomTargetIndex: func(int) (int, error) {
			return 0, errors.New("entropy failed")
		},
	}).Search(t.Context(), searchcore.Request{
		Terms: []string{"golang"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.PartialFailures) != 1 ||
		!strings.Contains(resp.PartialFailures[0].Reason, "entropy failed") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherReportsPeerFailures(t *testing.T) {
	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer badStatus.Close()
	malformed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureResponse(t, w, "count=bad\n")
	}))
	defer malformed.Close()

	resp, err := NewSearcher(Config{
		Client:      badStatus.Client(),
		NetworkName: "freeworld",
		Peers: fakePeerSource{peers: []yacymodel.Seed{
			serverSeed(t, badStatus.URL),
			serverSeed(t, malformed.URL),
			serverSeedWithHash(t, "http://127.0.0.1:1", yacymodel.WordHash("golang")),
		}},
		Concurrency: 2,
	}).Search(t.Context(), searchcore.Request{
		Terms:  []string{"golang"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.PartialFailures) != 3 {
		t.Fatalf("partial failures = %#v", resp.PartialFailures)
	}
}

func TestRemoteSearcherHonorsLimitAndOffset(t *testing.T) {
	first := hashFor("doc1")
	second := hashFor("doc2")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := yacyproto.SearchResponse{
			JoinCount: 2,
			Count:     2,
			Resources: []yacymodel.URIMetadataRow{
				metadataRow(t, first, "https://example.org/a", "A"),
				metadataRow(t, second, "https://example.org/b", "B"),
			},
		}
		writeFixtureResponse(t, w, response.Encode().Encode())
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yacymodel.Seed{serverSeed(t, server.URL)}},
	}).Search(t.Context(), searchcore.Request{Terms: []string{"golang"}, Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "B" {
		t.Fatalf("results = %#v", resp.Results)
	}
}

func TestRemoteSearcherReportsNormalizationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := yacyproto.SearchResponse{
			JoinCount: 1,
			Count:     1,
			Resources: []yacymodel.URIMetadataRow{{
				Properties: map[string]string{
					yacymodel.URLMetaHash: hashFor("doc1").String(),
					yacymodel.URLMetaURL: yacymodel.EncodeBase64WireForm(
						"https://example.org/",
					),
					yacymodel.URLMetaColDescription: "z|@@@",
				},
			}},
		}
		writeFixtureResponse(t, w, response.Encode().Encode())
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yacymodel.Seed{serverSeed(t, server.URL)}},
	}).Search(t.Context(), searchcore.Request{Terms: []string{"golang"}, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 0 || len(resp.PartialFailures) != 1 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherUsesTimeoutsAndPeerCap(t *testing.T) {
	word := yacymodel.WordHash("slow")
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
	}))
	defer slow.Close()
	unused := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("second peer should be capped out")
	}))
	defer unused.Close()

	resp, err := NewSearcher(Config{
		Client: slow.Client(),
		Peers: fakePeerSource{
			peers: []yacymodel.Seed{
				serverSeedWithHash(t, slow.URL, word),
				serverSeedWithHash(t, unused.URL, hashFor("ZZZZZZZZZZZZ")),
			},
		},
		MaxPeers:       1,
		Redundancy:     1,
		PerPeerTimeout: 5 * time.Millisecond,
		OverallTimeout: 20 * time.Millisecond,
	}).Search(t.Context(), searchcore.Request{Terms: []string{"slow"}, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.PartialFailures) != 1 {
		t.Fatalf("partial failures = %#v", resp.PartialFailures)
	}
}

func TestRemoteSearchReportsRequestConstructionFailure(t *testing.T) {
	original := newRemoteSearchRequest
	newRemoteSearchRequest = func(
		context.Context,
		string,
		string,
		io.Reader,
	) (*http.Request, error) {
		return nil, errors.New("bad request")
	}
	t.Cleanup(func() { newRemoteSearchRequest = original })

	_, err := NewSearcher(Config{}).(searcher).remoteSearch(
		t.Context(),
		serverSeed(t, "http://127.0.0.1:8090"),
		searchcore.Request{Limit: 1},
	)
	if err == nil {
		t.Fatal("expected request construction error")
	}
}

func TestRemoteSearchReportsTargetFailure(t *testing.T) {
	_, err := NewSearcher(Config{}).(searcher).remoteSearch(
		t.Context(),
		yacymodel.Seed{Hash: hashFor("missing")},
		searchcore.Request{Limit: 1},
	)
	if err == nil {
		t.Fatal("expected target error")
	}
}

func TestRemoteSearchReportsReadFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       failingBody{},
		}, nil
	})}

	_, err := NewSearcher(Config{Client: client}).(searcher).remoteSearch(
		t.Context(),
		serverSeed(t, "http://127.0.0.1:8090"),
		searchcore.Request{Limit: 1},
	)
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestReadRemoteSearchResponseRejectsOversizedBody(t *testing.T) {
	_, err := readRemoteSearchResponse(
		strings.NewReader(strings.Repeat("x", remoteSearchBodyCap+1)),
	)
	if err == nil {
		t.Fatal("expected oversized body error")
	}
}

func TestRemoteSearchRequestHelpers(t *testing.T) {
	req := remoteSearchRequest(searchcore.Request{
		Terms:            []string{"a"},
		ExcludedTerms:    []string{"b"},
		Limit:            7,
		ContentDomain:    searchcore.ContentDomainImage,
		Language:         "en",
		PreferMaskFilter: "prefer",
		URLMaskFilter:    "filter",
		SiteHost:         "example.org",
		FileType:         "pdf",
	}, "freeworld", 1500*time.Millisecond)
	if req.NetworkName != "freeworld" ||
		len(req.Query) != 1 ||
		len(req.Exclude) != 1 ||
		req.Count != 7 ||
		req.Time != 1500 ||
		req.ContentDom != yacyproto.ContentDomainImage ||
		req.FileType != "pdf" {
		t.Fatalf("request = %#v", req)
	}
	if positiveOrDefault(0, 3) != 3 || positiveOrDefault(2, 3) != 2 {
		t.Fatal("positiveOrDefault failed")
	}
	if durationOrDefault(0, time.Second) != time.Second ||
		durationOrDefault(time.Millisecond, time.Second) != time.Millisecond {
		t.Fatal("durationOrDefault failed")
	}
	if defaultMinimumPeerAgeDays(0) != DefaultMinimumPeerAgeDays ||
		defaultMinimumPeerAgeDays(-1) != -1 {
		t.Fatal("defaultMinimumPeerAgeDays failed")
	}
	if defaultMinimumPeerRWIs(0) != DefaultMinimumPeerRWIs ||
		defaultMinimumPeerRWIs(-1) != -1 {
		t.Fatal("defaultMinimumPeerRWIs failed")
	}
	if normalizedPartitionExponent(-1) != 0 ||
		normalizedPartitionExponent(maxPartitionExponent+1) != maxPartitionExponent ||
		normalizedPartitionExponent(2) != 2 {
		t.Fatal("normalizedPartitionExponent failed")
	}
	if got := peerFailure(yacymodel.Seed{}, context.Canceled); got.Source != "remote-yacy" {
		t.Fatalf("peer failure = %#v", got)
	}
}

func TestRemoteSearchAbstractHelpers(t *testing.T) {
	first := yacymodel.Hash("AAAAAA000000")
	second := yacymodel.Hash("AAAAAA000001")
	if got := intersectTermAbstracts(
		[]yacymodel.Hash{hashFor("a"), hashFor("b")},
		map[yacymodel.Hash]map[yacymodel.Hash]struct{}{
			hashFor("a"): {first: {}, second: {}},
			hashFor("b"): {second: {}},
		},
	); len(got) != 1 || got[0] != second {
		t.Fatalf("intersection = %v, want %s", got, second)
	}
	if got := intersectTermAbstracts(
		[]yacymodel.Hash{hashFor("a"), hashFor("b")},
		map[yacymodel.Hash]map[yacymodel.Hash]struct{}{
			hashFor("a"): {first: {}},
		},
	); got != nil {
		t.Fatalf("missing term intersection = %v, want nil", got)
	}
	if got := sortedHashes(map[yacymodel.Hash]struct{}{
		second: {},
		first:  {},
	}); len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("sorted hashes = %v", got)
	}
}

func TestRemoteSearchSecondaryURLLimits(t *testing.T) {
	many := make([]yacymodel.Hash, 0, secondaryURLCap+1)
	for i := range secondaryURLCap + 1 {
		many = append(many, hashFor(strconv.Itoa(i)))
	}
	if got := limitSecondaryURLs(
		searchcore.Request{},
		many,
	); len(
		got,
	) != searchcore.DefaultPublicLimit {
		t.Fatalf("default secondary urls = %d", len(got))
	}
	if got := limitSecondaryURLs(searchcore.Request{
		Offset: secondaryURLCap,
		Limit:  1,
	}, many); len(got) != secondaryURLCap {
		t.Fatalf("capped secondary urls = %d", len(got))
	}
	if got := limitSecondaryURLs(searchcore.Request{Limit: len(many)}, many[:1]); len(got) != 1 {
		t.Fatalf("uncapped secondary urls = %d", len(got))
	}
}

func TestRemoteSearchResultHelpers(t *testing.T) {
	first := yacymodel.Hash("AAAAAA000000")
	if got := remoteResultIdentity(
		searchcore.Result{URL: "https://example.org"},
	); got != "url:https://example.org" {
		t.Fatalf("remote result identity = %q", got)
	}
	deduped := dedupeSearchResults([]searchcore.Result{
		{URL: "https://example.org/a", URLHash: first.String()},
		{URL: "https://example.org/b", URLHash: first.String()},
		{URL: "https://example.org/a"},
		{URL: "https://example.org/a"},
	})
	if len(deduped) != 2 {
		t.Fatalf("deduped results = %#v", deduped)
	}
}

func TestDHTSearchPeerSelectionSkipsInvalidWordHash(t *testing.T) {
	peer := yacymodel.Seed{
		Hash:  hashFor("BBBBBBBBBBBB"),
		IP:    yacymodel.Some(mustHost(t, "192.0.2.1")),
		Port:  yacymodel.Some(yacymodel.Port(8090)),
		Flags: yacymodel.Some(acceptRemoteIndexFlags()),
	}
	got, err := selectDHTSearchPeers(
		[]yacymodel.Hash{yacymodel.Hash("bad"), peer.Hash},
		[]yacymodel.Seed{peer},
		dhtSearchPeerConfig{maxPeers: 1, redundancy: 1},
	)
	if err != nil {
		t.Fatalf("selectDHTSearchPeers: %v", err)
	}
	if len(got) != 1 || got[0].Hash != peer.Hash {
		t.Fatalf("selected peers = %#v", got)
	}
}

func TestDHTSearchPeerSelectionSkipsAlreadySeenTarget(t *testing.T) {
	peer := searchSeed(t, "BBBBBBBBBBBB")
	selected := []yacymodel.Seed{peer}
	seen := map[yacymodel.Hash]struct{}{peer.Hash: {}}

	err := appendDHTSearchPeers(
		&selected,
		seen,
		0,
		[]yacymodel.Seed{peer},
		dhtSearchPeerConfig{redundancy: 1, minimumPeerAgeDays: -1},
	)
	if err != nil {
		t.Fatalf("appendDHTSearchPeers: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected = %#v, want existing peer only", selected)
	}
}

func TestDHTSearchPeerSelectionRandomizesPeerCap(t *testing.T) {
	peers := []yacymodel.Seed{
		searchSeed(t, "AAAAAAAAAAAA"),
		searchSeed(t, "BBBBBBBBBBBB"),
		searchSeed(t, "CCCCCCCCCCCC"),
	}
	script := &searchTargetScript{values: []int{1}}
	got, err := limitDHTSearchPeers(peers, dhtSearchPeerConfig{
		maxPeers:          1,
		randomTargetIndex: script.next,
	})
	if err != nil {
		t.Fatalf("limitDHTSearchPeers: %v", err)
	}
	if len(got) != 1 || got[0].Hash != hashFor("BBBBBBBBBBBB") {
		t.Fatalf("selected peers = %#v", got)
	}
}

func TestDHTSearchPeerSelectionRejectsRandomErrors(t *testing.T) {
	peers := []yacymodel.Seed{
		searchSeed(t, "AAAAAAAAAAAA"),
		searchSeed(t, "BBBBBBBBBBBB"),
	}
	_, err := limitDHTSearchPeers(peers, dhtSearchPeerConfig{
		maxPeers: 1,
		randomTargetIndex: func(int) (int, error) {
			return 0, errors.New("entropy failed")
		},
	})
	if err == nil {
		t.Fatal("expected random target error")
	}
	_, err = limitDHTSearchPeers(peers, dhtSearchPeerConfig{
		maxPeers: 1,
		randomTargetIndex: func(int) (int, error) {
			return 2, nil
		},
	})
	if err == nil {
		t.Fatal("expected invalid random target index error")
	}
}

func TestSecureRandomTargetIndex(t *testing.T) {
	index, err := secureRandomTargetIndex(1)
	if err != nil {
		t.Fatalf("secureRandomTargetIndex: %v", err)
	}
	if index != 0 {
		t.Fatalf("index = %d, want 0", index)
	}

	saved := randomInteger
	randomInteger = func(io.Reader, *big.Int) (*big.Int, error) {
		return nil, errors.New("entropy failed")
	}
	t.Cleanup(func() { randomInteger = saved })
	if _, err := secureRandomTargetIndex(1); err == nil {
		t.Fatal("expected secure random error")
	}
}

func TestResultHelpers(t *testing.T) {
	host, pathValue, file := parsedURLParts(nil)
	if host != "" || pathValue != "" || file != "" {
		t.Fatalf("nil parts = %q %q %q", host, pathValue, file)
	}
	if displayURL("", "/path") != "/path" {
		t.Fatal("displayURL fallback failed")
	}
	hash := hashFor("doc1")
	result, err := searchResult(
		t.Context(),
		searchcore.Request{},
		metadataRow(t, hash, "not a url", ""),
		0,
		1,
	)
	if err != nil {
		t.Fatalf("searchResult: %v", err)
	}
	if result.Title != "not a url" || result.DisplayURL != "not%20a%20url" {
		t.Fatalf("result = %#v", result)
	}
	directoryHost, directoryPath, directoryFile := parsedURLParts(
		mustParseURL(t, "https://example.org/docs/"),
	)
	if directoryHost != "example.org" || directoryPath != "/docs/" || directoryFile != "" {
		t.Fatalf("directory parts = %q %q %q", directoryHost, directoryPath, directoryFile)
	}
	if _, err := searchResult(
		t.Context(),
		searchcore.Request{},
		yacymodel.URIMetadataRow{Properties: map[string]string{yacymodel.URLMetaHash: "bad"}},
		0,
		1,
	); err == nil {
		t.Fatal("expected bad hash error")
	}
	if _, err := searchResult(
		t.Context(),
		searchcore.Request{},
		yacymodel.URIMetadataRow{Properties: map[string]string{
			yacymodel.URLMetaHash:           hash.String(),
			yacymodel.URLMetaURL:            "z|@@@",
			yacymodel.URLMetaColDescription: yacymodel.EncodeBase64WireForm("bad url"),
		}},
		0,
		1,
	); err == nil {
		t.Fatal("expected bad url encoding error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (failingBody) Close() error {
	return nil
}

func mustParseURL(tb testing.TB, raw string) *url.URL {
	tb.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		tb.Fatalf("parse url: %v", err)
	}

	return parsed
}

func writeFixtureResponse(tb testing.TB, w http.ResponseWriter, body string) {
	tb.Helper()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := fixtureResponseTemplate.Execute(w, body); err != nil {
		tb.Fatalf("write fixture response: %v", err)
	}
}

func serverSeed(tb testing.TB, raw string) yacymodel.Seed {
	tb.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		tb.Fatalf("parse server url: %v", err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		tb.Fatalf("split host port: %v", err)
	}
	parsedHost, err := yacymodel.ParseHost(host)
	if err != nil {
		tb.Fatalf("parse host: %v", err)
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil {
		tb.Fatalf("parse port: %v", err)
	}

	return yacymodel.Seed{
		Hash:     hashFor("Peer" + port),
		IP:       yacymodel.Some(parsedHost),
		Port:     yacymodel.Some(yacymodel.Port(parsedPort)),
		Flags:    yacymodel.Some(acceptRemoteIndexFlags()),
		RWICount: yacymodel.Some(1),
	}
}

func serverSeedWithHash(tb testing.TB, raw string, hash yacymodel.Hash) yacymodel.Seed {
	tb.Helper()
	seed := serverSeed(tb, raw)
	seed.Hash = hash

	return seed
}

func metadataRow(
	tb testing.TB,
	hash yacymodel.Hash,
	rawURL string,
	title string,
) yacymodel.URIMetadataRow {
	tb.Helper()

	return yacymodel.URIMetadataRow{Properties: map[string]string{
		yacymodel.URLMetaHash:           hash.String(),
		yacymodel.URLMetaURL:            yacymodel.EncodeBase64WireForm(rawURL),
		yacymodel.URLMetaColDescription: yacymodel.EncodeBase64WireForm(title),
	}}
}

func hashFor(base string) yacymodel.Hash {
	const filler = "AAAAAAAAAAAA"
	if len(base) >= yacymodel.HashLength {
		return yacymodel.Hash(base[:yacymodel.HashLength])
	}

	return yacymodel.Hash(base + filler[len(base):])
}

func acceptRemoteIndexFlags() yacymodel.Flags {
	return yacymodel.ZeroFlags().Set(yacymodel.FlagAcceptRemoteIndex, true)
}

func searchSeed(tb testing.TB, raw string) yacymodel.Seed {
	tb.Helper()

	return yacymodel.Seed{
		Hash:     hashFor(raw),
		IP:       yacymodel.Some(mustHost(tb, "192.0.2.1")),
		Port:     yacymodel.Some(yacymodel.Port(8090)),
		Flags:    yacymodel.Some(acceptRemoteIndexFlags()),
		RWICount: yacymodel.Some(1),
	}
}

type searchTargetScript struct {
	values []int
}

func (s *searchTargetScript) next(int) (int, error) {
	value := s.values[0]
	s.values = s.values[1:]

	return value, nil
}

func mustHost(tb testing.TB, raw string) yacymodel.Host {
	tb.Helper()
	host, err := yacymodel.ParseHost(raw)
	if err != nil {
		tb.Fatalf("parse host %q: %v", raw, err)
	}

	return host
}
