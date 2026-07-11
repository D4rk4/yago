package searchremote

import (
	"context"
	"errors"
	"fmt"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

var fixtureResponseTemplate = template.Must(template.New("fixture").Parse("{{.}}"))

type fakePeerSource struct {
	peers []yagomodel.Seed
}

func (s fakePeerSource) SearchTargetPeers(context.Context) []yagomodel.Seed {
	return s.peers
}

func TestRemoteSearcherQueriesPeersAndNormalizesResults(t *testing.T) {
	word := yagomodel.WordHash("golang")
	hash := hashFor("doc1")
	self := searchSeed(t, "Self")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != yagoproto.PathSearch ||
			r.URL.Query().Get(yagoproto.FieldQuery) != word.String() ||
			r.URL.Query().Get(yagoproto.FieldNetworkName) != "freeworld" ||
			r.URL.Query().Get(yagoproto.FieldMySeed) == "" {
			t.Fatalf("request path/query = %s %s", r.URL.Path, r.URL.RawQuery)
		}
		response := yagoproto.SearchResponse{
			JoinCount: 1,
			Count:     1,
			Resources: []yagomodel.URIMetadataRow{
				metadataRow(t, hash, "https://example.org/doc.html", "Remote Result"),
			},
		}
		message := response.Encode()
		yagoproto.InjectResponseHeader(message, "1.940", 42)
		writeFixtureResponse(t, w, message.Encode())
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
		SelfSeed: func(context.Context) yagomodel.Seed {
			return self
		},
	}).Search(t.Context(), searchcore.Request{
		Terms:         []string{"golang"},
		Source:        searchcore.SourceGlobal,
		Limit:         10,
		ContentDomain: searchcore.ContentDomainText,
		Verify:        searchcore.VerifyFalse,
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
		result.Score != 1.0/61.0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestRemoteSearcherSelectsDHTTargetByWordHash(t *testing.T) {
	word := yagomodel.WordHash("golang")
	unused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("peer outside the DHT target set should not be queried")
	}))
	defer unused.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get(yagoproto.FieldQuery) != word.String() {
			t.Fatalf("query = %q, want %q", r.URL.Query().Get(yagoproto.FieldQuery), word)
		}
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}))
	defer target.Close()

	resp, err := NewSearcher(Config{
		Client:      target.Client(),
		NetworkName: "freeworld",
		Peers: fakePeerSource{peers: []yagomodel.Seed{
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
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
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
	if resp.TotalResults != 1 ||
		len(resp.Results) != 1 ||
		resp.Results[0].URLHash != fixture.shared.String() ||
		len(resp.PartialFailures) != 0 {
		t.Fatalf("response = %#v", resp)
	}
	if requests := fixture.recordedRequests(); len(requests) != 5 {
		t.Fatalf("request count = %d, want 5 (1 primary + 2 abstract + 2 secondary); requests=%v",
			len(requests), requests)
	}
}

// TestRemoteSearcherMultiWordPrimaryConjunction proves the fix for multi-word
// queries returning zero results: the primary search now sends every query word
// hash to the peer, which returns the AND-intersection directly, so a hit is
// found without depending on the fragile index-abstract intersection.
func TestRemoteSearcherMultiWordPrimaryConjunction(t *testing.T) {
	doc := hashFor("doc")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form := r.URL.Query()
		if form.Get(yagoproto.FieldAbstracts) != "" || form.Get(yagoproto.FieldURLs) != "" {
			// Abstract/secondary enhancement: no split results in this fixture.
			writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())

			return
		}
		// Primary conjunction search returns a document matching every word.
		writeFixtureResponse(t, w, yagoproto.SearchResponse{
			JoinCount: 1,
			Count:     1,
			Resources: []yagomodel.URIMetadataRow{
				metadataRow(t, doc, "https://example.org/doc", "Doc alpha beta"),
			},
		}.Encode().Encode())
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
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
	if len(resp.Results) != 1 || resp.Results[0].URLHash != doc.String() {
		t.Fatalf("primary conjunction did not return the multi-word result: %#v", resp)
	}
}

type multiTermAbstractFixture struct {
	tb        testing.TB
	alpha     yagomodel.Hash
	beta      yagomodel.Hash
	shared    yagomodel.Hash
	alphaOnly yagomodel.Hash
	betaOnly  yagomodel.Hash
	requests  []url.Values
	mu        sync.Mutex
}

func newMultiTermAbstractFixture(tb testing.TB) *multiTermAbstractFixture {
	tb.Helper()

	return &multiTermAbstractFixture{
		tb:        tb,
		alpha:     yagomodel.WordHash("alpha"),
		beta:      yagomodel.WordHash("beta"),
		shared:    hashFor("shared"),
		alphaOnly: hashFor("alpha-only"),
		betaOnly:  hashFor("beta-only"),
	}
}

func (f *multiTermAbstractFixture) serve(w http.ResponseWriter, r *http.Request) {
	form := r.URL.Query()
	f.record(form)

	switch {
	case form.Get(yagoproto.FieldQuery) == "" &&
		form.Get(yagoproto.FieldAbstracts) == f.alpha.String():
		f.writeAbstract(w, f.alpha, f.alphaOnly)
	case form.Get(yagoproto.FieldQuery) == "" &&
		form.Get(yagoproto.FieldAbstracts) == f.beta.String():
		f.writeAbstract(w, f.beta, f.betaOnly)
	case form.Get(yagoproto.FieldQuery) == f.alpha.String() &&
		form.Get(yagoproto.FieldURLs) == f.shared.String():
		f.writeSecondary(w)
	case form.Get(yagoproto.FieldQuery) == f.beta.String() &&
		form.Get(yagoproto.FieldURLs) == f.shared.String():
		f.writeSecondary(w)
	case form.Get(yagoproto.FieldAbstracts) == "" &&
		form.Get(yagoproto.FieldURLs) == "":
		// primary conjunction search: this fixture holds no single-peer hit, so
		// the shared document is recovered only via the abstract/secondary phase.
		writeFixtureResponse(f.tb, w, yagoproto.SearchResponse{}.Encode().Encode())
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
	term yagomodel.Hash,
	termOnly yagomodel.Hash,
) {
	writeFixtureResponse(f.tb, w, yagoproto.SearchResponse{
		IndexCount: map[yagomodel.Hash]int{term: 2},
		IndexAbstract: map[yagomodel.Hash]string{
			term: yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{
				termOnly,
				f.shared,
			}),
		},
	}.Encode().Encode())
}

func (f *multiTermAbstractFixture) writeSecondary(w http.ResponseWriter) {
	writeFixtureResponse(f.tb, w, yagoproto.SearchResponse{
		JoinCount: 1,
		Count:     1,
		Resources: []yagomodel.URIMetadataRow{
			metadataRow(f.tb, f.shared, "https://example.org/shared", "Shared alpha beta"),
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
	if len(resp.PartialFailures) != 1 ||
		resp.PartialFailures[0].Reason != "no peer source configured" {
		t.Fatalf("response = %#v", resp)
	}

	resp, err = NewSearcher(Config{Peers: fakePeerSource{}}).Search(
		t.Context(),
		searchcore.Request{Terms: []string{"golang"}, Source: searchcore.SourceGlobal, Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search with empty roster: %v", err)
	}
	if len(resp.PartialFailures) != 1 || resp.PartialFailures[0].Reason != "no known peers" {
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
		Peers:  fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, unused.URL)}},
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
	// The primary conjunction search runs first for every query, so a missing
	// peer source short-circuits before the abstract phase with one failure.
	if len(resp.PartialFailures) != 1 ||
		!strings.Contains(resp.PartialFailures[0].Reason, "no peer source configured") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherReportsNoDHTTargets(t *testing.T) {
	unused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("ineligible peer should not be queried")
	}))
	defer unused.Close()
	peer := serverSeed(t, unused.URL)
	peer.Flags = yagomodel.Some(yagomodel.ZeroFlags())

	resp, err := NewSearcher(Config{
		Client: unused.Client(),
		Peers:  fakePeerSource{peers: []yagomodel.Seed{peer}},
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
	alpha := yagomodel.WordHash("alpha")
	beta := yagomodel.WordHash("beta")
	at := time.Unix(1_800_000_000, 0).UTC()
	sink := &capturedReputationObservations{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get(yagoproto.FieldAbstracts) {
		case alpha.String():
			writeFixtureResponse(t, w, yagoproto.SearchResponse{
				IndexAbstract: map[yagomodel.Hash]string{alpha: "{bad"},
			}.Encode().Encode())
		case beta.String():
			writeFixtureResponse(t, w, yagoproto.SearchResponse{
				IndexAbstract: map[yagomodel.Hash]string{
					beta: yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{hashFor("beta")}),
				},
			}.Encode().Encode())
		case "":
			// primary conjunction search: no direct hit in this fixture.
			writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
		default:
			t.Fatalf("unexpected request: %s", r.URL.RawQuery)
		}
	}))
	defer server.Close()
	peer := serverSeed(t, server.URL)

	resp, err := NewSearcher(Config{
		Client:                 server.Client(),
		Peers:                  fakePeerSource{peers: []yagomodel.Seed{peer}},
		MaxPeers:               1,
		Redundancy:             1,
		Concurrency:            1,
		ReputationObservations: sink,
		ReputationClock:        func() time.Time { return at },
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
	if len(sink.batches) != 1 || len(sink.batches[0]) != 3 ||
		countReputationOutcome(sink.batches[0], peerreputation.OutcomeSuccess) != 2 ||
		countReputationOutcome(sink.batches[0], peerreputation.OutcomeInvalidResult) != 1 {
		t.Fatalf("abstract observations = %#v", sink.batches)
	}
}

func TestRemoteSearcherReportsIndexAbstractPeerFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
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
	// One primary conjunction failure plus the abstract phase (two peer failures
	// and two missing-response notices) surface together.
	if len(resp.Results) != 0 ||
		len(resp.PartialFailures) != 5 ||
		!strings.Contains(resp.PartialFailures[0].Reason, "status 502") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherReportsMissingIndexAbstractResponses(t *testing.T) {
	term := yagomodel.WordHash("alpha")
	abstracts, failures := (searcher{}).termAbstracts(
		t.Context(),
		searchcore.Request{},
		[]termPeerTargets{{term: term}},
		nil,
	)
	if len(abstracts) != 0 ||
		len(failures) != 1 ||
		!strings.Contains(failures[0].Reason, "no index abstract responses") {
		t.Fatalf("abstracts=%#v failures=%#v", abstracts, failures)
	}
}

func TestRemoteSearcherTermTargetsReportsMissingTargets(t *testing.T) {
	term := yagomodel.WordHash("alpha")
	targets, failures := (searcher{}).termTargets(t.Context(), []yagomodel.Hash{term})
	if len(targets) != 0 ||
		len(failures) != 1 ||
		!strings.Contains(failures[0].Reason, "no dht search targets for") {
		t.Fatalf("targets=%#v failures=%#v", targets, failures)
	}
}

func TestRemoteSearcherSkipsPeersWithoutRWIInventory(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("empty RWI peer should not be queried")
	}))
	defer empty.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}))
	defer target.Close()

	emptySeed := serverSeed(t, empty.URL)
	emptySeed.RWICount = yagomodel.Some(0)

	resp, err := NewSearcher(Config{
		Client: target.Client(),
		Peers: fakePeerSource{peers: []yagomodel.Seed{
			emptySeed,
			serverSeedWithHash(t, target.URL, yagomodel.WordHash("golang")),
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
		Peers: fakePeerSource{peers: []yagomodel.Seed{
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
		Peers: fakePeerSource{peers: []yagomodel.Seed{
			serverSeed(t, badStatus.URL),
			serverSeed(t, malformed.URL),
			serverSeedWithHash(t, "http://127.0.0.1:1", yagomodel.WordHash("golang")),
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
		response := yagoproto.SearchResponse{
			JoinCount: 2,
			Count:     2,
			Resources: []yagomodel.URIMetadataRow{
				metadataRow(t, first, "https://example.org/a", "A golang"),
				metadataRow(t, second, "https://example.org/b", "B golang"),
			},
		}
		writeFixtureResponse(t, w, response.Encode().Encode())
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
	}).Search(t.Context(), searchcore.Request{Terms: []string{"golang"}, Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "B golang" {
		t.Fatalf("results = %#v", resp.Results)
	}
}

func TestRemoteSearcherReportsNormalizationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := yagoproto.SearchResponse{
			JoinCount: 1,
			Count:     1,
			Resources: []yagomodel.URIMetadataRow{{
				Properties: map[string]string{
					yagomodel.URLMetaHash: hashFor("doc1").String(),
					yagomodel.URLMetaURL: yagomodel.EncodeBase64WireForm(
						"https://example.org/",
					),
					yagomodel.URLMetaColDescription: "z|@@@",
				},
			}},
		}
		writeFixtureResponse(t, w, response.Encode().Encode())
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
	}).Search(t.Context(), searchcore.Request{Terms: []string{"golang"}, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 0 || len(resp.PartialFailures) != 1 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRemoteSearcherUsesTimeoutsAndPeerCap(t *testing.T) {
	word := yagomodel.WordHash("slow")
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
			peers: []yagomodel.Seed{
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
		yagomodel.Seed{Hash: hashFor("missing")},
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
		req.ContentDom != yagoproto.ContentDomainImage ||
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
	if got := peerFailure(yagomodel.Seed{}, context.Canceled); got.Source != "remote-yacy" {
		t.Fatalf("peer failure = %#v", got)
	}
}

func TestRemoteSearchAbstractHelpers(t *testing.T) {
	first := yagomodel.Hash("AAAAAA000000")
	second := yagomodel.Hash("AAAAAA000001")
	if got := intersectTermAbstracts(
		[]yagomodel.Hash{hashFor("a"), hashFor("b")},
		map[yagomodel.Hash]map[yagomodel.Hash]struct{}{
			hashFor("a"): {first: {}, second: {}},
			hashFor("b"): {second: {}},
		},
	); len(got) != 1 || got[0] != second {
		t.Fatalf("intersection = %v, want %s", got, second)
	}
	if got := intersectTermAbstracts(
		[]yagomodel.Hash{hashFor("a"), hashFor("b")},
		map[yagomodel.Hash]map[yagomodel.Hash]struct{}{
			hashFor("a"): {first: {}},
		},
	); got != nil {
		t.Fatalf("missing term intersection = %v, want nil", got)
	}
	if got := sortedHashes(map[yagomodel.Hash]struct{}{
		second: {},
		first:  {},
	}); len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("sorted hashes = %v", got)
	}
}

func TestRemoteSearchSecondaryURLLimits(t *testing.T) {
	many := make([]yagomodel.Hash, 0, secondaryURLCap+1)
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
	first := yagomodel.Hash("AAAAAA000000")
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
	peer := yagomodel.Seed{
		Hash:  hashFor("BBBBBBBBBBBB"),
		IP:    yagomodel.Some(mustHost(t, "192.0.2.1")),
		Port:  yagomodel.Some(yagomodel.Port(8090)),
		Flags: yagomodel.Some(acceptRemoteIndexFlags()),
	}
	got, err := selectDHTSearchPeers(
		[]yagomodel.Hash{yagomodel.Hash("bad"), peer.Hash},
		[]yagomodel.Seed{peer},
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
	selected := []yagomodel.Seed{peer}
	seen := map[yagomodel.Hash]struct{}{peer.Hash: {}}

	err := appendDHTSearchPeers(
		&selected,
		seen,
		0,
		[]yagomodel.Seed{peer},
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
	peers := []yagomodel.Seed{
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
	peers := []yagomodel.Seed{
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
		yagomodel.URIMetadataRow{Properties: map[string]string{yagomodel.URLMetaHash: "bad"}},
	); err == nil {
		t.Fatal("expected bad hash error")
	}
	if _, err := searchResult(
		t.Context(),
		searchcore.Request{},
		yagomodel.URIMetadataRow{Properties: map[string]string{
			yagomodel.URLMetaHash:           hash.String(),
			yagomodel.URLMetaURL:            "z|@@@",
			yagomodel.URLMetaColDescription: yagomodel.EncodeBase64WireForm("bad url"),
		}},
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

func serverSeed(tb testing.TB, raw string) yagomodel.Seed {
	tb.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		tb.Fatalf("parse server url: %v", err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		tb.Fatalf("split host port: %v", err)
	}
	parsedHost, err := yagomodel.ParseHost(host)
	if err != nil {
		tb.Fatalf("parse host: %v", err)
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil {
		tb.Fatalf("parse port: %v", err)
	}

	return yagomodel.Seed{
		Hash:     hashFor("Peer" + port),
		IP:       yagomodel.Some(parsedHost),
		Port:     yagomodel.Some(yagomodel.Port(parsedPort)),
		Flags:    yagomodel.Some(acceptRemoteIndexFlags()),
		RWICount: yagomodel.Some(1),
	}
}

func serverSeedWithHash(tb testing.TB, raw string, hash yagomodel.Hash) yagomodel.Seed {
	tb.Helper()
	seed := serverSeed(tb, raw)
	seed.Hash = hash

	return seed
}

func metadataRow(
	tb testing.TB,
	hash yagomodel.Hash,
	rawURL string,
	title string,
) yagomodel.URIMetadataRow {
	tb.Helper()

	return yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash:           hash.String(),
		yagomodel.URLMetaURL:            yagomodel.EncodeBase64WireForm(rawURL),
		yagomodel.URLMetaColDescription: yagomodel.EncodeBase64WireForm(title),
	}}
}

func hashFor(base string) yagomodel.Hash {
	const filler = "AAAAAAAAAAAA"
	if len(base) >= yagomodel.HashLength {
		return yagomodel.Hash(base[:yagomodel.HashLength])
	}

	return yagomodel.Hash(base + filler[len(base):])
}

func acceptRemoteIndexFlags() yagomodel.Flags {
	return yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true)
}

func searchSeed(tb testing.TB, raw string) yagomodel.Seed {
	tb.Helper()

	return yagomodel.Seed{
		Hash:     hashFor(raw),
		IP:       yagomodel.Some(mustHost(tb, "192.0.2.1")),
		Port:     yagomodel.Some(yagomodel.Port(8090)),
		Flags:    yagomodel.Some(acceptRemoteIndexFlags()),
		RWICount: yagomodel.Some(1),
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

func mustHost(tb testing.TB, raw string) yagomodel.Host {
	tb.Helper()
	host, err := yagomodel.ParseHost(raw)
	if err != nil {
		tb.Fatalf("parse host %q: %v", raw, err)
	}

	return host
}

func TestRemoteSearcherExpandsSingleWordMorphology(t *testing.T) {
	var mu sync.Mutex
	queries := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get(yagoproto.FieldQuery)
		mu.Lock()
		queries[query] = true
		mu.Unlock()
		writeFixtureResponse(t, w, yagoproto.SearchResponse{
			JoinCount: 1,
			Count:     1,
			Resources: []yagomodel.URIMetadataRow{
				metadataRow(
					t,
					hashFor(query),
					"https://example.org/"+query+".html",
					"R черногория",
				),
			},
		}.Encode().Encode())
	}))
	defer server.Close()

	resp, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
		ExpandWord: func(word string) []string {
			return []string{word, word + "и", word + "ю"}
		},
	}).Search(t.Context(), searchcore.Request{
		Terms:         []string{"черногория"},
		Source:        searchcore.SourceGlobal,
		Limit:         1,
		ContentDomain: searchcore.ContentDomainText,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	mu.Lock()
	distinct := len(queries)
	mu.Unlock()
	if distinct < 2 {
		t.Fatalf("expected several inflected-form queries, got %d", distinct)
	}
	// Each variant returned a distinct document; the fused list is capped to the
	// requested limit.
	if len(resp.Results) != 1 {
		t.Fatalf("variant fusion not capped to the limit: %d", len(resp.Results))
	}
}

func TestSwarmMorphologyVariantsGating(t *testing.T) {
	expanding := searcher{expandWord: func(word string) []string {
		return []string{word, word + "и"}
	}}
	// A single word expands to several surface variants.
	if got := expanding.swarmMorphologyVariants(
		searchcore.Request{Terms: []string{"черногория"}},
	); len(got) < 2 {
		t.Fatalf("single word not expanded: %v", got)
	}
	// A multi-word query keeps the exact conjunctive search.
	if got := expanding.swarmMorphologyVariants(
		searchcore.Request{Terms: []string{"black", "mountain"}},
	); got != nil {
		t.Fatalf("multi-word expanded: %v", got)
	}
	// No expander wired: no expansion at all.
	if got := (searcher{}).swarmMorphologyVariants(
		searchcore.Request{Terms: []string{"черногория"}},
	); got != nil {
		t.Fatalf("nil expander expanded: %v", got)
	}
}

// TestFanOutQueriesEveryPeerConcurrently pins SEARCH-37 (YaCy parity:
// RemoteSearch starts one thread per selected peer): thirteen peers each
// taking 400ms must all be answered well inside a budget that four sequential
// waves would blow — with the old four-worker pool this test times out.
func TestFanOutQueriesEveryPeerConcurrently(t *testing.T) {
	const peerCount = 13
	var inFlight, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			observed := peak.Load()
			if current <= observed || peak.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(400 * time.Millisecond)
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		_, _ = w.Write([]byte("resource=\nlinkcount=0\nreferences=\n"))
	}))
	defer server.Close()

	peers := make([]yagomodel.Seed, 0, peerCount)
	for i := range peerCount {
		peers = append(peers, serverSeedWithHash(
			t,
			server.URL,
			yagomodel.Hash(fmt.Sprintf("FanOutPeer%02d", i)),
		))
	}
	searcher := NewSearcher(Config{
		Client:         server.Client(),
		NetworkName:    "freeworld",
		Peers:          fakePeerSource{peers: peers},
		MaxPeers:       peerCount,
		Redundancy:     peerCount,
		PerPeerTimeout: 2 * time.Second,
		OverallTimeout: 1500 * time.Millisecond,
	})

	start := time.Now()
	_, err := searcher.Search(t.Context(), searchcore.Request{
		Terms:  []string{"solo"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1200*time.Millisecond {
		t.Fatalf("fan-out took %v — peers are being queried in waves", elapsed)
	}
	if got := peak.Load(); got < peerCount {
		t.Fatalf("peak concurrency = %d, want all %d peers in flight together", got, peerCount)
	}
}
