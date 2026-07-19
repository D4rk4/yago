package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestAdminSearchMapsTypedFiltersToCanonicalRequest(t *testing.T) {
	t.Parallel()

	searcher := &stubAdminSearcher{}
	_, err := newSearchSource(searcher).Search(context.Background(), adminui.SearchQuery{
		Query:  "bounded site:query.example language:de",
		Global: true,
		Filters: adminui.SearchFilters{
			ContentDomain: "image",
			Language:      "ru",
			SiteHost:      "form.example",
		},
		Offset: 20,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	request := searcher.gotRequest
	if request.Source != searchcore.SourceGlobal ||
		request.ContentDomain != searchcore.ContentDomainImage ||
		request.Language != "ru" || request.SiteHost != "form.example" ||
		request.Offset != 20 || request.Limit != 20 ||
		request.Verify != searchcore.VerifyIfExist {
		t.Fatalf("request = %+v", request)
	}
}

func TestAdminSearchRejectsInvalidTypedContentDomain(t *testing.T) {
	t.Parallel()

	searcher := &stubAdminSearcher{}
	_, err := newSearchSource(searcher).Search(context.Background(), adminui.SearchQuery{
		Query: "bounded",
		Filters: adminui.SearchFilters{
			ContentDomain: "book",
		},
		Limit: 20,
	})
	if err == nil {
		t.Fatal("invalid content domain was accepted")
	}
	if searcher.gotRequest.Query != "" {
		t.Fatalf("invalid request reached searcher: %+v", searcher.gotRequest)
	}
}
