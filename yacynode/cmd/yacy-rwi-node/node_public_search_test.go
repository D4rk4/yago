package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

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
	mountNodePublicSearch(mux, nodeStorage{
		postings:     publicSearchPostingIndex{},
		urlDirectory: publicSearchURLDirectory{},
	}, nil, nodeidentity.Identity{NetworkName: "freeworld"}, http.DefaultClient)

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
