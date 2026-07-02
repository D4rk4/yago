package main

import (
	"context"
	"html/template"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

var publicSearchResponseTemplate = template.Must(template.New("response").Parse("{{.}}"))

type publicSearchPostingIndex struct{}

func (publicSearchPostingIndex) RWICount(context.Context) (int, error) {
	return 0, nil
}

func (publicSearchPostingIndex) ScanWord(
	context.Context,
	yacymodel.Hash,
	func(yacymodel.RWIPosting) (bool, error),
) error {
	return nil
}

type publicSearchURLDirectory struct{}

func (publicSearchURLDirectory) RowsByHash(
	context.Context,
	[]yacymodel.Hash,
) ([]yacymodel.URIMetadataRow, error) {
	return nil, nil
}

func (publicSearchURLDirectory) MissingURLs(
	context.Context,
	[]yacymodel.Hash,
) ([]yacymodel.Hash, error) {
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
		identity: nodeidentity.Identity{NetworkName: "freeworld"},
		dht:      defaultPublicSearchDHTConfig(),
		client:   http.DefaultClient,
	})

	for _, path := range []string{
		yacyproto.PathYaCySearchJSON + "?query=absent",
		yacyproto.PathYaCySearchRSS + "?query=absent",
		yacyproto.PathYaCySearchHTML + "?query=absent",
		yacyproto.PathOpenSearch,
		yacyproto.PathSuggestJSON + "?query=absent",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestNodePublicSearchUsesDHTRedundancyConfig(t *testing.T) {
	word := yacymodel.WordHash("golang")
	unused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("redundancy 1 should not query the second DHT peer")
	}))
	defer unused.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != yacyproto.PathSearch ||
			r.URL.Query().Get(yacyproto.FieldQuery) != word.String() {
			t.Fatalf("remote query = %s %s", r.URL.Path, r.URL.RawQuery)
		}
		if err := publicSearchResponseTemplate.Execute(
			w,
			yacyproto.SearchResponse{}.Encode().Encode(),
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
		roster: reachableRoster{peers: []yacymodel.Seed{
			publicSearchSeed(t, unused.URL, yacymodel.Hash("ZZZZZZZZZZZZ")),
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
		yacyproto.PathYaCySearchJSON+"?query=golang&resource=global",
		nil,
	)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
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
	hash yacymodel.Hash,
) yacymodel.Seed {
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
		Hash:     hash,
		IP:       yacymodel.Some(parsedHost),
		Port:     yacymodel.Some(yacymodel.Port(parsedPort)),
		Flags:    yacymodel.Some(publicSearchAcceptRemoteIndexFlags()),
		RWICount: yacymodel.Some(1),
	}
}

func publicSearchAcceptRemoteIndexFlags() yacymodel.Flags {
	return yacymodel.ZeroFlags().Set(yacymodel.FlagAcceptRemoteIndex, true)
}
