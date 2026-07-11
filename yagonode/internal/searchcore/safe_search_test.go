package searchcore

import (
	"errors"
	"testing"
)

func TestSafeSearchFiltersExplicitAndUntrustedUnknownResults(t *testing.T) {
	inner := stubSearcher{response: Response{TotalResults: 7, Results: []Result{
		{URL: "general", Source: SourceLocal, SafetyRating: SafetyGeneral},
		{URL: "explicit", Source: SourceLocal, SafetyRating: SafetyExplicit},
		{URL: "local-unknown", Source: SourceLocal},
		{URL: "remote-unknown", Source: SourceRemote},
		{URL: "web-unknown", Source: SourceWeb},
		{URL: "remote-general", Source: SourceRemote, SafetyRating: SafetyGeneral},
		{URL: "invalid-remote", Source: SourceRemote, SafetyRating: SafetyRating(99)},
	}}}
	response, err := NewSafeSearchSearcher(inner).Search(
		t.Context(),
		Request{SafeSearch: true, ContentDomain: ContentDomainText},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := urls(response.Results); len(got) != 3 || got[0] != "general" ||
		got[1] != "local-unknown" || got[2] != "remote-general" ||
		response.TotalResults != 3 || !response.Request.SafeSearch {
		t.Fatalf("response = %#v", response)
	}
}

func TestSafeSearchFiltersUnknownImageAndCanBeDisabled(t *testing.T) {
	inner := stubSearcher{response: Response{TotalResults: 2, Results: []Result{
		{URL: "unknown", Source: SourceLocal},
		{URL: "general", Source: SourceLocal, SafetyRating: SafetyGeneral},
	}}}
	strict, err := NewSafeSearchSearcher(inner).Search(
		t.Context(),
		Request{SafeSearch: true, ContentDomain: ContentDomainImage},
	)
	if err != nil || len(strict.Results) != 1 || strict.Results[0].URL != "general" {
		t.Fatalf("strict image response = %#v/%v", strict, err)
	}
	byResult, err := NewSafeSearchSearcher(stubSearcher{response: Response{Results: []Result{{
		URL: "image", Source: SourceLocal, ContentDomain: ContentDomainImage,
	}}}}).Search(t.Context(), Request{SafeSearch: true})
	if err != nil || len(byResult.Results) != 0 {
		t.Fatalf("result-domain response = %#v/%v", byResult, err)
	}
	disabled, err := NewSafeSearchSearcher(inner).Search(t.Context(), Request{})
	if err != nil || len(disabled.Results) != 2 || disabled.TotalResults != 2 {
		t.Fatalf("disabled response = %#v/%v", disabled, err)
	}
}

func TestSafeSearchPropagatesInnerError(t *testing.T) {
	if _, err := NewSafeSearchSearcher(stubSearcher{err: errors.New("failed")}).Search(
		t.Context(),
		Request{SafeSearch: true},
	); err == nil {
		t.Fatal("expected inner error")
	}
}
