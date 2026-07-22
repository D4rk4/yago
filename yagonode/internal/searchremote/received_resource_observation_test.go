package searchremote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestSaltedRemoteSearchCountsReceivedResources(t *testing.T) {
	self := searchSeed(t, "observer")
	access := yagoproto.NetworkAccess{
		NetworkName: "private",
		Mode:        yagoproto.NetworkAuthenticationSaltedMagic,
		Essentials:  "shared-secret",
	}
	var authorized atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !access.Authorizes(r.URL.Query()) ||
			r.URL.Query().Get(yagoproto.FieldIam) != self.Hash.String() {
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}
		authorized.Store(true)
		response := yagoproto.SearchResponse{
			JoinCount: 2,
			Count:     2,
			Resources: []yagomodel.URIMetadataRow{
				metadataRow(t, hashFor("resource-1"), "https://example.org/one", "One"),
				metadataRow(t, hashFor("resource-2"), "https://example.org/two", "Two"),
			},
		}
		writeFixtureResponse(t, w, response.Encode().Encode())
	}))
	defer server.Close()

	var observations atomic.Int32
	var resources atomic.Int64
	response, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "private",
		Peers:       fakePeerSource{peers: []yagomodel.Seed{serverSeed(t, server.URL)}},
		SelfSeed: func(context.Context) yagomodel.Seed {
			return self
		},
		NetworkAccess: access,
		ObserveReceivedResources: func(_ context.Context, received int) {
			observations.Add(1)
			resources.Add(int64(received))
		},
	}).Search(t.Context(), searchcore.Request{
		Terms:  []string{"resource"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !authorized.Load() || len(response.PartialFailures) != 0 {
		t.Fatalf("authorized = %t, response = %#v", authorized.Load(), response)
	}
	if observations.Load() != 1 || resources.Load() != 2 {
		t.Fatalf(
			"received resource observations = %d/%d, want 1/2",
			observations.Load(),
			resources.Load(),
		)
	}
}

func TestFailedRemoteSearchDoesNotCountReceivedResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var observations atomic.Int32
	_, err := NewSearcher(Config{
		Client: server.Client(),
		ObserveReceivedResources: func(context.Context, int) {
			observations.Add(1)
		},
	}).(searcher).remoteSearch(
		t.Context(),
		serverSeed(t, server.URL),
		searchcore.Request{Terms: []string{"resource"}, Limit: 10},
	)
	if err == nil {
		t.Fatal("failed remote response was accepted")
	}
	if observations.Load() != 0 {
		t.Fatalf("failed response observations = %d, want 0", observations.Load())
	}
}

func TestSaltedRemoteSearchRequiresSelfIdentity(t *testing.T) {
	searcher := NewSearcher(Config{
		NetworkAccess: yagoproto.NetworkAccess{
			Mode: yagoproto.NetworkAuthenticationSaltedMagic,
		},
	}).(searcher)
	_, _, err := searcher.sendRemoteSearchWithinLimit(
		t.Context(),
		yagomodel.Seed{},
		yagoproto.SearchRequest{},
		remoteSearchRequestLimits{responseBodyLimit: remoteSearchBodyCap},
	)
	if !errors.Is(err, errRemoteSearchFailed) ||
		!strings.Contains(err.Error(), "missing self identity") {
		t.Fatalf("missing self identity error = %v", err)
	}
}

func TestSaltedRemoteSearchSurfacesSigningFailure(t *testing.T) {
	sentinel := errors.New("signing failed")
	searcher := NewSearcher(Config{
		SelfSeed: func(context.Context) yagomodel.Seed {
			return searchSeed(t, "self")
		},
		NetworkAccess: yagoproto.NetworkAccess{
			Mode: yagoproto.NetworkAuthenticationSaltedMagic,
		},
	}).(searcher)
	searcher.signNetworkForm = func(yagoproto.NetworkAccess, url.Values) error {
		return sentinel
	}
	_, _, err := searcher.sendRemoteSearchWithinLimit(
		t.Context(),
		yagomodel.Seed{},
		yagoproto.SearchRequest{},
		remoteSearchRequestLimits{responseBodyLimit: remoteSearchBodyCap},
	)
	if !errors.Is(err, sentinel) || !errors.Is(err, errRemoteSearchFailed) {
		t.Fatalf("remote search signing error = %v", err)
	}
}
