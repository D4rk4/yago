package yagonode

import (
	"context"
	"html/template"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchremote"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
	"github.com/D4rk4/yago/yagoproto"
)

var publicSearchResponseTemplate = template.Must(template.New("response").Parse("{{.}}"))

type publicSearchPostingIndex struct{}

func (publicSearchPostingIndex) RWICount(context.Context) (int, error) {
	return 0, nil
}

func (publicSearchPostingIndex) RWIURLCount(context.Context, yagomodel.Hash) (int, error) {
	return 0, nil
}

func (publicSearchPostingIndex) ScanWord(
	context.Context,
	yagomodel.Hash,
	func(yagomodel.RWIPosting) (bool, error),
) error {
	return nil
}

type publicSearchURLDirectory struct{}

func (publicSearchURLDirectory) RowsByHash(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	return nil, nil
}

func (publicSearchURLDirectory) MissingURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return nil, nil
}

func (publicSearchURLDirectory) Count(context.Context) (int, error) {
	return 0, nil
}

func TestNodePublicSearchMountsYaCySearchSurfaces(t *testing.T) {
	mux := http.NewServeMux()
	mountNodePublicSearch(mux, publicSearchAssembly{
		storage: nodeStorage{
			postings:     publicSearchPostingIndex{},
			urlDirectory: publicSearchURLDirectory{},
		},
		identity:     nodeidentity.Identity{NetworkName: "freeworld"},
		dht:          defaultPublicSearchDHTConfig(),
		client:       http.DefaultClient,
		clickCapture: true,
	})

	for _, path := range []string{
		yagoproto.PathYaCySearchJSON + "?query=absent",
		yagoproto.PathYaCySearchRSS + "?query=absent",
		yagoproto.PathYaCySearchHTML + "?query=absent",
		yagoproto.PathOpenSearch,
		yagoproto.PathSuggestJSON + "?query=absent",
		yagoproto.PathSuggestXML + "?query=absent",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}

	// SEC-02: the agent /search surface has no configured credential here, so
	// it must deny rather than serve publicly; the YaCy-compatible surfaces
	// above stay public by design.
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/search",
		strings.NewReader(`{"query":"absent","max_results":1}`),
	)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/search without a key: status = %d, want 401 (no key, no access)", rec.Code)
	}
}

func TestNodePublicSearchAppliesSearchAPIKeyOnlyToAgentSearch(t *testing.T) {
	mux := http.NewServeMux()
	mountNodePublicSearch(mux, publicSearchAssembly{
		storage: nodeStorage{
			postings:     publicSearchPostingIndex{},
			urlDirectory: publicSearchURLDirectory{},
		},
		identity:     nodeidentity.Identity{NetworkName: "freeworld"},
		dht:          defaultPublicSearchDHTConfig(),
		client:       http.DefaultClient,
		searchAPIKey: "secret",
	})

	yacyRec := httptest.NewRecorder()
	yacyReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathYaCySearchJSON+"?query=absent",
		nil,
	)
	mux.ServeHTTP(yacyRec, yacyReq)
	if yacyRec.Code != http.StatusOK {
		t.Fatalf("YaCy search status = %d body=%s", yacyRec.Code, yacyRec.Body.String())
	}

	missingRec := httptest.NewRecorder()
	missingReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/search",
		strings.NewReader(`{"query":"absent"}`),
	)
	mux.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d body=%s", missingRec.Code, missingRec.Body.String())
	}

	authorizedRec := httptest.NewRecorder()
	authorizedReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/search",
		strings.NewReader(`{"query":"absent"}`),
	)
	authorizedReq.Header.Set("Authorization", "Bearer secret")
	mux.ServeHTTP(authorizedRec, authorizedReq)
	if authorizedRec.Code != http.StatusServiceUnavailable ||
		authorizedRec.Header().Get("Retry-After") != "1" {
		t.Fatalf("authorized status = %d body=%s", authorizedRec.Code, authorizedRec.Body.String())
	}
}

func TestNodePublicSearchUsesDHTRedundancyConfig(t *testing.T) {
	word := yagomodel.WordHash("golang")
	unused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("redundancy 1 should not query the second DHT peer")
	}))
	defer unused.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != yagoproto.PathSearch ||
			r.URL.Query().Get(yagoproto.FieldQuery) != word.String() {
			t.Fatalf("remote query = %s %s", r.URL.Path, r.URL.RawQuery)
		}
		if err := publicSearchResponseTemplate.Execute(
			w,
			yagoproto.SearchResponse{}.Encode().Encode(),
		); err != nil {
			t.Fatalf("write remote response: %v", err)
		}
	}))
	defer target.Close()

	mux := http.NewServeMux()
	mountNodePublicSearch(mux, publicSearchAssembly{
		storage: nodeStorage{
			postings:     publicSearchPostingIndex{},
			urlDirectory: publicSearchURLDirectory{},
		},
		roster: reachableRoster{peers: []yagomodel.Seed{
			publicSearchSeed(t, unused.URL, yagomodel.Hash("ZZZZZZZZZZZZ")),
			publicSearchSeed(t, target.URL, word),
		}},
		identity: nodeidentity.Identity{NetworkName: "freeworld"},
		dht: dhtDistributionConfig{
			Redundancy:         1,
			MinimumPeerAgeDays: -1,
		},
		client: target.Client(),
		dhtSearchTargetIndex: func(int) (int, error) {
			return 0, nil
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathYaCySearchJSON+"?query=golang&resource=global",
		nil,
	)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestNodePublicSearchInstallsRemoteResultCache(t *testing.T) {
	searcher, _ := mountNodePublicSearch(http.NewServeMux(), publicSearchAssembly{
		storage: nodeStorage{
			searchIndex:  stubSearchIndex{},
			postings:     publicSearchPostingIndex{},
			urlDirectory: publicSearchURLDirectory{},
		},
		identity:           nodeidentity.Identity{NetworkName: "freeworld"},
		dht:                defaultPublicSearchDHTConfig(),
		client:             http.DefaultClient,
		indexRemoteResults: true,
	})

	parsed, ok := searcher.(parsedQuerySearcher)
	if !ok {
		t.Fatalf("searcher = %T, want a parsedQuerySearcher", searcher)
	}
	effective, ok := parsed.inner.(effectiveWebFallbackRequestSearcher)
	if !ok {
		t.Fatalf("parsed inner = %T, want an effectiveWebFallbackRequestSearcher", parsed.inner)
	}
	continuity, ok := effective.inner.(searchsession.RecentSuccessSearcher)
	if !ok {
		t.Fatalf("parsed inner = %T, want a RecentSuccessSearcher", parsed.inner)
	}
	budgeted, ok := continuity.Inner.(interactiveBudgetSearcher)
	if !ok {
		t.Fatalf("continuity inner = %T, want an interactiveBudgetSearcher", continuity.Inner)
	}
	if _, ok := budgeted.inner.(remoteCachingSearcher); !ok {
		t.Fatalf(
			"inner searcher = %T, want a remoteCachingSearcher when result caching is enabled",
			budgeted.inner,
		)
	}
}

func portalGateRequest(t *testing.T, enabled bool) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	toggles := &runtimeToggles{}
	if enabled {
		toggles.SetPortalEnabled(true)
	}
	hit := false
	gate := portalGate(toggles, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/opensearch.xml",
		nil,
	))

	return rec, hit
}

func TestRemoteRankingWeightsNarrowLocalProfile(t *testing.T) {
	weights := remoteRankingWeights(func() searchindex.RankingWeights {
		return searchindex.RankingWeights{Title: 7, Headings: 5, Anchors: 3, Body: 2, URL: 6}
	})()
	if weights != (searchremote.RankingWeights{Title: 7, URL: 6}) {
		t.Fatalf("weights = %#v", weights)
	}
	if got := remoteRankingWeights(nil)(); got != searchremote.DefaultRankingWeights() {
		t.Fatalf("nil provider weights = %#v", got)
	}
}

func TestPortalGateBlocksWhenDisabled(t *testing.T) {
	rec, hit := portalGateRequest(t, false)
	if hit {
		t.Fatal("gate served a portal route while the portal was disabled")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rec.Code)
	}
}

func TestPortalGateServesWhenEnabled(t *testing.T) {
	_, hit := portalGateRequest(t, true)
	if !hit {
		t.Fatal("gate blocked a portal route while the portal was enabled")
	}
}

func defaultPublicSearchDHTConfig() dhtDistributionConfig {
	return dhtDistributionConfig{
		Redundancy:         defaultDHTRedundancy,
		PartitionExponent:  defaultDHTPartitionExponent,
		MinimumPeerAgeDays: 3,
	}
}

func publicSearchSeed(
	tb testing.TB,
	raw string,
	hash yagomodel.Hash,
) yagomodel.Seed {
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
		Hash:     hash,
		IP:       yagomodel.Some(parsedHost),
		Port:     yagomodel.Some(yagomodel.Port(parsedPort)),
		Flags:    yagomodel.Some(publicSearchAcceptRemoteIndexFlags()),
		RWICount: yagomodel.Some(1),
	}
}

func publicSearchAcceptRemoteIndexFlags() yagomodel.Flags {
	return yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true)
}
