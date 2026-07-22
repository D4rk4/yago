package searchremote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestRemoteSearchRequestCarriesFeasibleOperators(t *testing.T) {
	request := baseRemoteSearchRequest(searchcore.Request{
		Language:         "de",
		SiteHost:         "example.org",
		Author:           "Ada Lovelace",
		FileType:         "pdf",
		URLMaskFilter:    `https://example\.org/.*`,
		PreferMaskFilter: "docs",
	}, "freeworld", 1200*time.Millisecond)
	form := request.Form()
	want := map[string]string{
		yagoproto.FieldLanguage: "de",
		yagoproto.FieldSiteHost: "example.org",
		yagoproto.FieldAuthor:   "Ada Lovelace",
		yagoproto.FieldFileType: "pdf",
		yagoproto.FieldFilter:   `https://example\.org/.*`,
		yagoproto.FieldPrefer:   "docs",
	}
	for field, value := range want {
		if got := form.Get(field); got != value {
			t.Fatalf("%s = %q, want %q", field, got, value)
		}
	}
}

func TestOpenRemoteSearchSendsIamWithMySeedInOneRound(t *testing.T) {
	self := searchSeed(t, "self")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		form := r.URL.Query()
		if form.Get(yagoproto.FieldIam) != self.Hash.String() ||
			form.Get(yagoproto.FieldMySeed) == "" {
			t.Fatalf("identity form = %v", form)
		}
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}))
	defer server.Close()

	_, err := NewSearcher(Config{
		Client: server.Client(),
		SelfSeed: func(context.Context) yagomodel.Seed {
			return self
		},
	}).(searcher).remoteSearch(
		t.Context(),
		serverSeed(t, server.URL),
		searchcore.Request{Terms: []string{"term"}, Limit: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("requests = %d, want 1", calls.Load())
	}
}
