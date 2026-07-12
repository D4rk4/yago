package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type capturingSearcher struct{ got searchcore.Request }

func (s *capturingSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.got = req

	return searchcore.Response{}, nil
}

func TestParsedQueryAppliesOperatorsForRawQuerySurfaces(t *testing.T) {
	inner := &capturingSearcher{}
	submitted := `author:doe site:example.org filetype:pdf tld:de inurl:blog language:ru /date near golang tools`
	_, err := withParsedQuery(inner).Search(context.Background(), searchcore.Request{
		Query: submitted,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	got := inner.got
	if got.Author != "doe" || got.SiteHost != "example.org" || got.FileType != "pdf" ||
		got.TLD != "de" || got.InURL != "blog" || got.Language != "ru" ||
		!got.SortByDate || !got.Near {
		t.Fatalf("operators not applied: %+v", got)
	}
	if got.Query != "golang tools" {
		t.Fatalf("query = %q, want the bare terms so the index never sees operators", got.Query)
	}
	if got.SubmittedQuery != submitted {
		t.Fatalf("submitted query = %q", got.SubmittedQuery)
	}
}

func TestParsedQueryKeepsCallerSuppliedFields(t *testing.T) {
	inner := &capturingSearcher{}
	_, err := withParsedQuery(inner).Search(context.Background(), searchcore.Request{
		Query:    "site:parsed.example golang",
		SiteHost: "caller.example",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if inner.got.SiteHost != "caller.example" {
		t.Fatalf("caller-supplied site overridden: %q", inner.got.SiteHost)
	}
}
