package tavilyapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// answeredWithLostPeer is the shape of nearly every search a live swarm node
// runs: the sources returned a result and at least one peer was lost. No
// search observed on the production node in thirty hours completed with zero
// partial failures, so this is the ordinary case, not the exceptional one.
func answeredWithLostPeer() searchcore.Response {
	return searchcore.Response{
		Results: []searchcore.Result{{
			Title:        "Metadata title",
			URL:          "https://blocked.example/doc",
			Snippet:      "metadata snippet",
			Score:        9.5,
			Host:         "blocked.example",
			Date:         "2020-01-01",
			SafetyRating: searchcore.SafetyGeneral,
		}},
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceRemoteYaCy,
			Reason: "deadline",
		}},
	}
}

// peerAnsweredWithLostPeer is the same ordinary shape answered by a peer instead
// of from local storage. First-seen is local by nature -- peers neither send nor
// receive it -- so this is the row a first-seen window must drop.
func peerAnsweredWithLostPeer() searchcore.Response {
	response := answeredWithLostPeer()
	response.Results[0].Source = searchcore.SourceRemote

	return response
}

func serveSearchBody(
	t *testing.T,
	response searchcore.Response,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		PathSearch,
		strings.NewReader(body),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	newTestSearchEndpoint(
		&fakeSearcher{response: response},
		&fakeDocuments{rows: map[string]documentstore.Document{}},
	).ServeHTTP(rec, req)

	return rec
}

// TestSearchServesEmptyAnswerWhenCallerFiltersMatchNothing pins the difference
// between "the sources did not complete" and "your own constraints matched
// nothing". Availability used to be scored on the count left after the date,
// domain, and exact-match filters ran, so a request that narrowed its own
// result set to zero was answered 503 with Retry-After -- advice to retry a
// query whose outcome is deterministic. Each filter here removes the single
// returned result, and each must answer 200 with an empty list.
func TestSearchServesEmptyAnswerWhenCallerFiltersMatchNothing(t *testing.T) {
	withScriptedFilterClock(t)
	for _, test := range []struct {
		name     string
		body     string
		response searchcore.Response
	}{
		{
			name:     "date bound",
			body:     `{"query":"golang","time_range":"week"}`,
			response: answeredWithLostPeer(),
		},
		{
			name:     "news topic default bound",
			body:     `{"query":"golang","topic":"news"}`,
			response: answeredWithLostPeer(),
		},
		{
			name:     "explicit start date",
			body:     `{"query":"golang","start_date":"2026-01-01"}`,
			response: answeredWithLostPeer(),
		},
		{
			name:     "excluded domain",
			body:     `{"query":"golang","exclude_domains":["blocked.example"]}`,
			response: answeredWithLostPeer(),
		},
		{
			name:     "included domain matching nothing",
			body:     `{"query":"golang","include_domains":["allowed.example"]}`,
			response: answeredWithLostPeer(),
		},
		{
			name:     "exact match phrase absent",
			body:     `{"query":"golang \"phrase that never appears\"","exact_match":true}`,
			response: answeredWithLostPeer(),
		},
		{
			// A first-seen window that retains no row is the same kind of
			// deterministic zero: the sources answered, and the caller's own
			// window kept nothing. It must not be reported as an unavailable
			// search, and it must not advise a retry.
			name:     "first-seen window",
			body:     `{"query":"golang","first_seen_start":"2026-01-01"}`,
			response: peerAnsweredWithLostPeer(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := serveSearchBody(t, test.response, test.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Retry-After"); got != "" {
				t.Fatalf("Retry-After = %q, want none on a complete answer", got)
			}
			if results := decodeSearchResponse(t, rec).Results; len(results) != 0 {
				t.Fatalf("results = %#v, want the caller's filter to have kept nothing", results)
			}
		})
	}
}

// TestSearchStillRefusesZeroItCannotVouchFor is the other half of the guard
// above: moving availability off the served count must not stop the node
// refusing an answer it genuinely cannot vouch for. The sources here returned
// nothing at all alongside a lost peer, which no caller filter can explain.
func TestSearchStillRefusesZeroItCannotVouchFor(t *testing.T) {
	withScriptedFilterClock(t)
	rec := serveSearchBody(
		t,
		searchcore.Response{PartialFailures: answeredWithLostPeer().PartialFailures},
		`{"query":"golang","time_range":"week"}`,
	)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
}

// TestSearchServesQueryShapeMissWithoutRetryAdvice covers the query the DHT
// cannot be asked at all. ParseTextQuery consumes site:, inurl: and tld: as
// modifiers, so "site:example.com" reaches the remote stage carrying no word
// hashes. Reporting that as a lost peer answered a legitimate query with 503
// and Retry-After, advice that no retry could act on because the outcome is
// fixed by the query itself.
func TestSearchServesQueryShapeMissWithoutRetryAdvice(t *testing.T) {
	rec := serveSearchBody(
		t,
		searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceQueryShape,
			Reason: "no query terms",
		}}},
		`{"query":"site:example.com"}`,
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want none for a query the DHT cannot serve", got)
	}
}

// TestSearchStillRefusesWhenAQueryShapeMissHidesARealLoss is the over-permitting
// guard: a query-shape failure must not license ignoring a peer that really was
// lost in the same search.
func TestSearchStillRefusesWhenAQueryShapeMissHidesARealLoss(t *testing.T) {
	rec := serveSearchBody(
		t,
		searchcore.Response{PartialFailures: []searchcore.PartialFailure{
			{Source: searchcore.PartialFailureSourceQueryShape, Reason: "no query terms"},
			{Source: searchcore.PartialFailureSourceRemoteYaCy, Reason: "deadline"},
		}},
		`{"query":"site:example.com"}`,
	)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
}

// TestSearchServesTruthfulZeroWithoutRetryAdvice keeps the third case honest:
// every source answered and found nothing, so the empty list is the truth and
// carries no retry advice.
func TestSearchServesTruthfulZeroWithoutRetryAdvice(t *testing.T) {
	rec := serveSearchBody(t, searchcore.Response{}, `{"query":"golang"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want none on a truthful zero", got)
	}
}
