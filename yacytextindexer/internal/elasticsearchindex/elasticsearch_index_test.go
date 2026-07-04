package elasticsearchindex_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/elasticsearchindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/searchdocument"
)

func TestElasticsearchIndexPutsDocumentByID(t *testing.T) {
	var gotPath string
	var gotDoc searchdocument.SearchDocument
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotDoc); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	index := elasticsearchindex.NewElasticsearchIndex(server.URL, "yacy-text", server.Client())
	page := yacycrawlcontract.CrawledPage{
		CanonicalURL: "https://example.com/",
		DocumentID:   "abc123",
		Title:        "Hi",
		Text:         "words here",
		CrawledAt:    time.Unix(0, 0).UTC(),
		Language:     "en",
	}
	if err := index.Index(context.Background(), page); err != nil {
		t.Fatalf("index: %v", err)
	}
	if gotPath != "/yacy-text/_doc/abc123" {
		t.Errorf("path = %q", gotPath)
	}
	if gotDoc.Title != "Hi" || gotDoc.URL != "https://example.com/" ||
		gotDoc.Content != "words here" {
		t.Errorf("document = %+v", gotDoc)
	}
}

func TestElasticsearchIndexReturnsErrorOnFailureStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	index := elasticsearchindex.NewElasticsearchIndex(server.URL, "yacy-text", server.Client())
	err := index.Index(context.Background(), yacycrawlcontract.CrawledPage{DocumentID: "abc123"})
	if err == nil {
		t.Fatal("expected error for non-2xx response")
	}
}
