package searchremote

import (
	"context"
	"errors"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
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

func TestRemoteSearcherDeduplicatesDHTTargetsAcrossTerms(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeFixtureResponse(t, w, yacyproto.SearchResponse{}.Encode().Encode())
	}))
	defer server.Close()

	_, err := NewSearcher(Config{
		Client:     server.Client(),
		Peers:      fakePeerSource{peers: []yacymodel.Seed{serverSeed(t, server.URL)}},
		Redundancy: 1,
	}).Search(t.Context(), searchcore.Request{
		Terms: []string{"golang", "yacy"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
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

func TestRemoteSearchHelpers(t *testing.T) {
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
	if normalizedPartitionExponent(-1) != 0 ||
		normalizedPartitionExponent(maxPartitionExponent+1) != maxPartitionExponent ||
		normalizedPartitionExponent(2) != 2 {
		t.Fatal("normalizedPartitionExponent failed")
	}
	if got := peerFailure(yacymodel.Seed{}, context.Canceled); got.Source != "remote-yacy" {
		t.Fatalf("peer failure = %#v", got)
	}
}

func TestDHTSearchPeerSelectionSkipsInvalidWordHash(t *testing.T) {
	peer := yacymodel.Seed{
		Hash:  hashFor("BBBBBBBBBBBB"),
		IP:    yacymodel.Some(mustHost(t, "192.0.2.1")),
		Port:  yacymodel.Some(yacymodel.Port(8090)),
		Flags: yacymodel.Some(acceptRemoteIndexFlags()),
	}
	got := selectDHTSearchPeers(
		[]yacymodel.Hash{yacymodel.Hash("bad"), peer.Hash},
		[]yacymodel.Seed{peer},
		dhtSearchPeerConfig{maxPeers: 1, redundancy: 1},
	)
	if len(got) != 1 || got[0].Hash != peer.Hash {
		t.Fatalf("selected peers = %#v", got)
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
		Hash:  hashFor("Peer" + port),
		IP:    yacymodel.Some(parsedHost),
		Port:  yacymodel.Some(yacymodel.Port(parsedPort)),
		Flags: yacymodel.Some(acceptRemoteIndexFlags()),
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

func mustHost(tb testing.TB, raw string) yacymodel.Host {
	tb.Helper()
	host, err := yacymodel.ParseHost(raw)
	if err != nil {
		tb.Fatalf("parse host %q: %v", raw, err)
	}

	return host
}
