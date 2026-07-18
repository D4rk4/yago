package searchsession

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type overReturningSearcher struct{}

func (overReturningSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	results := make([]searchcore.Result, req.Limit+1)
	for index := range results {
		results[index] = searchcore.Result{
			URL:     fmt.Sprintf("https://example.test/%d", index),
			URLHash: fmt.Sprintf("hash-%d", index),
		}
	}

	return searchcore.Response{TotalResults: maxSessionDepth, Results: results}, nil
}

type payloadSearcher struct {
	response searchcore.Response
}

func (s *payloadSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return s.response, nil
}

func TestStableWindowDetachesAndIsolatesCompletePayload(t *testing.T) {
	backing := strings.Repeat("x", 1<<20)
	short := backing[:8]
	inner := &payloadSearcher{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title:            short,
			URL:              "https://example.test/",
			Images:           []searchcore.ResultImage{{URL: "image", Alt: "alt"}},
			QueryMatches:     []searchcore.QueryMatch{{Start: 1, End: 2}},
			BodyQueryMatches: []searchcore.QueryMatch{{Start: 3, End: 4}},
			FieldScores: map[string]float64{
				"body": 1,
			},
			FieldTermPositions: map[string]map[string][]int{
				"body": {"term": {1, 2}},
			},
		}},
		PartialFailures: []searchcore.PartialFailure{{Source: "peer", Reason: "late"}},
		Facets: []searchcore.FacetGroup{{
			Name: "host", Terms: []searchcore.FacetTerm{{Term: "example.test", Count: 1}},
		}},
	}}
	stable := WithStableWindow(inner).(*stableSearcher)
	req := searchcore.Request{Query: "go", Limit: 1}
	first, err := stable.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	entry := stable.sessions[sessionKey(req)]
	if entry == nil || entry.retained >= len(backing) {
		t.Fatalf("detached session/bytes = %#v/%d", entry, entry.retained)
	}
	first.Results[0].Title = "changed"
	first.Results[0].Images[0].URL = "changed"
	first.Results[0].QueryMatches[0].Start = 9
	first.Results[0].BodyQueryMatches[0].Start = 9
	first.Results[0].FieldScores["body"] = 9
	first.Results[0].FieldTermPositions["body"]["term"][0] = 9
	first.PartialFailures[0].Reason = "changed"
	first.Facets[0].Terms[0].Term = "changed"
	inner.response.Results[0].Images[0].Alt = "changed"
	inner.response.Results[0].QueryMatches[0].End = 9
	inner.response.Results[0].BodyQueryMatches[0].End = 9
	inner.response.Results[0].FieldScores["body"] = 7
	inner.response.Results[0].FieldTermPositions["body"]["term"][1] = 7
	inner.response.PartialFailures[0].Source = "changed"
	inner.response.Facets[0].Name = "changed"

	cached := entry.respond(req)
	result := cached.Results[0]
	if result.Title != short || result.Images[0] != (searchcore.ResultImage{
		URL: "image", Alt: "alt",
	}) || result.QueryMatches[0] != (searchcore.QueryMatch{Start: 1, End: 2}) ||
		result.BodyQueryMatches[0] != (searchcore.QueryMatch{Start: 3, End: 4}) ||
		result.FieldScores["body"] != 1 ||
		result.FieldTermPositions["body"]["term"][0] != 1 ||
		result.FieldTermPositions["body"]["term"][1] != 2 ||
		cached.PartialFailures[0] != (searchcore.PartialFailure{
			Source: "peer", Reason: "late",
		}) || cached.Facets[0].Name != "host" ||
		cached.Facets[0].Terms[0].Term != "example.test" {
		t.Fatalf("cached payload changed: %#v", cached)
	}
}

func TestAppendUnseenDetachesCandidatePayload(t *testing.T) {
	backing := strings.Repeat("x", 1<<20)
	candidates := []searchcore.Result{{
		Title:  backing[:4],
		URL:    "new",
		Images: []searchcore.ResultImage{{URL: "image", Alt: "alt"}},
		FieldScores: map[string]float64{
			"body": 1,
		},
		FieldTermPositions: map[string]map[string][]int{
			"body": {"term": {1}},
		},
	}}
	appended := appendUnseen(nil, candidates, 1)
	candidates[0].Images[0].URL = "changed"
	candidates[0].FieldScores["body"] = 2
	candidates[0].FieldTermPositions["body"]["term"][0] = 2
	if appended[0].Title != backing[:4] || appended[0].Images[0].URL != "image" ||
		appended[0].FieldScores["body"] != 1 ||
		appended[0].FieldTermPositions["body"]["term"][0] != 1 {
		t.Fatalf("appended payload changed: %#v", appended[0])
	}
	entry := &session{results: appended}
	if retainedSessionBytes(entry) >= len(backing) {
		t.Fatalf("appended result retained backing: %d", retainedSessionBytes(entry))
	}
}

func TestSessionPayloadClonePreservesNilCollections(t *testing.T) {
	result := cloneSessionResult(searchcore.Result{
		FieldTermPositions: map[string]map[string][]int{
			"title": nil,
			"body":  {"term": nil},
		},
	})
	facets := cloneSessionFacets([]searchcore.FacetGroup{{Name: "host"}})
	if cloneSessionResults(nil) != nil || cloneSessionFailures(nil) != nil ||
		cloneSessionFacets(nil) != nil || result.Images != nil || result.QueryMatches != nil ||
		result.BodyQueryMatches != nil ||
		result.FieldScores != nil ||
		result.FieldTermPositions["title"] != nil ||
		result.FieldTermPositions["body"]["term"] != nil || facets[0].Terms != nil {
		t.Fatalf("nil collections changed: %#v/%#v", result, facets)
	}
}

func TestSessionPayloadClonePreservesAuthoritativeEmptyQueryMatches(t *testing.T) {
	results := cloneSessionResults([]searchcore.Result{{
		QueryMatches: []searchcore.QueryMatch{},
	}})
	if results[0].QueryMatches == nil || len(results[0].QueryMatches) != 0 {
		t.Fatalf("QueryMatches = %#v", results[0].QueryMatches)
	}
}

func TestStableWindowBoundsOverReturningExtension(t *testing.T) {
	stable := WithStableWindow(overReturningSearcher{})
	if _, err := stable.Search(context.Background(), searchcore.Request{
		Query: "go", Limit: 10,
	}); err != nil {
		t.Fatal(err)
	}
	response, err := stable.Search(context.Background(), searchcore.Request{
		Query: "go", Offset: 50, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 10 || response.TotalResults != maxSessionDepth {
		t.Fatalf("bounded response = %d/%d", len(response.Results), response.TotalResults)
	}
}
