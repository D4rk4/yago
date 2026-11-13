package searchremote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
	"github.com/D4rk4/yago/yagoproto"
)

func peerAnswerSeed(t *testing.T, handler http.HandlerFunc) yagomodel.Seed {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return serverSeed(t, server.URL)
}

func answeredPeerFailures(failures []searchcore.PartialFailure) int {
	answered := 0
	for _, failure := range failures {
		if failure.Diagnostic.SourceAnswered {
			answered++
		}
	}

	return answered
}

func TestRemoteSearchMarksPeerLossAnsweredOnlyByAnotherPeersCompleteAnswer(t *testing.T) {
	emptyAnswer := func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}
	rowAnswer := func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureResponse(t, w, searchResponse(
			metadataRow(t, hashFor("answer"), "https://example.org/answer", "golang"),
		).Encode().Encode())
	}
	incompleteAnswer := func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureResponse(t, w, yagoproto.SearchResponse{
			Count: 2,
			Resources: []yagomodel.URIMetadataRow{
				metadataRow(t, hashFor("partial"), "https://example.org/partial", "golang"),
			},
		}.Encode().Encode())
	}
	malformedAnswer := func(w http.ResponseWriter, _ *http.Request) {
		writeFixtureResponse(t, w, "count=bad\n")
	}
	refusal := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}
	for _, test := range []struct {
		name     string
		peers    []http.HandlerFunc
		stage    searchcore.FailureStage
		failures int
		answered int
		results  int
	}{
		{
			name: "empty answer beside a lost peer", peers: []http.HandlerFunc{emptyAnswer, refusal},
			stage: searchcore.FailureStageRemotePeer, failures: 1, answered: 1,
		},
		{
			name: "complete rows beside a lost peer", peers: []http.HandlerFunc{rowAnswer, refusal},
			stage: searchcore.FailureStageRemotePeer, failures: 1, answered: 1, results: 1,
		},
		{
			name: "every peer lost", peers: []http.HandlerFunc{refusal, refusal},
			stage: searchcore.FailureStageRemotePeer, failures: 2,
		},
		{
			name: "malformed answer beside a lost peer", peers: []http.HandlerFunc{malformedAnswer, refusal},
			stage: searchcore.FailureStageRemotePeer, failures: 2,
		},
		{
			name: "incomplete answer beside a lost peer", peers: []http.HandlerFunc{incompleteAnswer, refusal},
			stage: searchcore.FailureStageRemotePeer, failures: 1, results: 1,
		},
		{name: "every peer answered", peers: []http.HandlerFunc{emptyAnswer, rowAnswer}, results: 1},
		{name: "no targets", stage: searchcore.FailureStageRemoteSearch, failures: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			peers := make([]yagomodel.Seed, 0, len(test.peers))
			for _, handler := range test.peers {
				peers = append(peers, peerAnswerSeed(t, handler))
			}
			response, err := NewSearcher(Config{
				NetworkName: "freeworld",
				Peers:       fakePeerSource{peers: peers},
			}).Search(t.Context(), searchcore.Request{
				Terms:  []string{"golang"},
				Source: searchcore.SourceGlobal,
				Limit:  10,
			})
			if err != nil || len(response.Results) != test.results ||
				len(response.PartialFailures) != test.failures ||
				answeredPeerFailures(response.PartialFailures) != test.answered ||
				response.LostSourceFailures() != test.failures {
				t.Fatalf("response = %#v, error = %v", response, err)
			}
			for _, failure := range response.PartialFailures {
				if failure.Diagnostic.Stage != test.stage {
					t.Fatalf("failure = %#v", failure)
				}
			}
		})
	}
}

func TestPeerAnswerAvailabilityAdmitsOnlyCompleteWholeQueryAnswers(t *testing.T) {
	peerLoss := errors.Join(errRemoteSearchFailed, errRemoteSearchTransport)
	lostPeer := peerSearchResult{wholeQuery: true, err: peerLoss}
	for _, test := range []struct {
		name     string
		answer   peerSearchResult
		loss     peerSearchResult
		failures int
		answered int
	}{
		{
			name: "complete empty answer", answer: peerSearchResult{wholeQuery: true},
			loss: lostPeer, failures: 1, answered: 1,
		},
		{
			name:   "locally truncated answer",
			answer: peerSearchResult{wholeQuery: true, resourcesTruncated: true},
			loss:   lostPeer, failures: 1,
		},
		{
			name:     "incomplete answer",
			answer:   peerSearchResult{wholeQuery: true, responseIncomplete: true},
			loss:     lostPeer,
			failures: 1,
		},
		{
			name: "declared rows missing",
			answer: peerSearchResult{
				wholeQuery: true,
				response:   yagoproto.SearchResponse{Count: 1},
			},
			loss:     lostPeer,
			failures: 1,
		},
		{
			name: "invalid row",
			answer: peerSearchResult{
				wholeQuery: true,
				response:   searchResponse(yagomodel.URIMetadataRow{}),
			},
			loss:     lostPeer,
			failures: 2,
		},
		{name: "secondary answer", answer: peerSearchResult{}, loss: lostPeer, failures: 1},
		{
			name:     "secondary loss",
			answer:   peerSearchResult{wholeQuery: true},
			loss:     peerSearchResult{err: peerLoss},
			failures: 1,
		},
		{
			name:   "locally cut call",
			answer: peerSearchResult{wholeQuery: true},
			loss: peerSearchResult{
				wholeQuery: true,
				err:        errRemoteSearchBudgetExhausted,
			},
			failures: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.answer.peer = yagomodel.Seed{Hash: hashFor("answering")}
			test.answer.countDeclared = true
			test.loss.peer = yagomodel.Seed{Hash: hashFor("lost")}
			response, _ := (searcher{weights: weightsOrDefault(nil)}).responseWithinBudget(
				t.Context(),
				searchcore.Request{Limit: 10},
				[]peerSearchResult{test.answer, test.loss},
				nil,
				newRemoteQueryBudget(),
			)
			if len(response.PartialFailures) != test.failures ||
				answeredPeerFailures(response.PartialFailures) != test.answered {
				t.Fatalf("failures = %#v", response.PartialFailures)
			}
		})
	}
}

func TestRemoteSearchKeepsVariantPeerLossUnansweredByAnotherVariant(t *testing.T) {
	base := yagomodel.WordHash("golang").String()
	emptyAnswer := func(w http.ResponseWriter) {
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}
	refusal := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}
	for _, test := range []struct {
		name            string
		answeredQueries func(query string) bool
		failures        int
		answered        int
	}{
		{
			name:            "only the base variant answered",
			answeredQueries: func(query string) bool { return query == base },
			failures:        3,
		},
		{
			name:            "only the later variant answered",
			answeredQueries: func(query string) bool { return query != base },
			failures:        3,
		},
		{
			name:            "every variant answered",
			answeredQueries: func(string) bool { return true },
			failures:        2,
			answered:        2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			answering := func(w http.ResponseWriter, r *http.Request) {
				if !test.answeredQueries(r.URL.Query().Get(yagoproto.FieldQuery)) {
					refusal(w, r)

					return
				}
				emptyAnswer(w)
			}
			response, err := NewSearcher(Config{
				NetworkName: "freeworld",
				Peers: fakePeerSource{peers: []yagomodel.Seed{
					peerAnswerSeed(t, answering),
					peerAnswerSeed(t, refusal),
				}},
				ExpandWord: func(word string) []string { return []string{word, word + "s"} },
			}).Search(t.Context(), searchcore.Request{
				Terms:  []string{"golang"},
				Source: searchcore.SourceGlobal,
				Limit:  10,
			})
			if err != nil || len(response.PartialFailures) != test.failures ||
				answeredPeerFailures(response.PartialFailures) != test.answered {
				t.Fatalf("response = %#v, error = %v", response, err)
			}
			assertVariantPeerAnswersSurviveSessionMerge(t, response, test.answered/2)
		})
	}
}

func assertVariantPeerAnswersSurviveSessionMerge(
	t *testing.T,
	response searchcore.Response,
	answered int,
) {
	t.Helper()
	original := slices.Clone(response.PartialFailures)
	response.PartialFailures = append(response.PartialFailures, searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceFuzzyStage,
		Reason: "deadline",
	})
	merged, err := searchsession.WithRecentSuccessOnIncompleteRefresh(
		peerAnswerRefresh{response: response}, peerAnswerRecent{},
	).Search(t.Context(), response.Request)
	if err != nil || answeredPeerFailures(merged.PartialFailures) != answered ||
		!slices.Equal(original, response.PartialFailures[:len(original)]) {
		t.Fatalf("merged failures = %#v, error = %v", merged.PartialFailures, err)
	}
}

type peerAnswerRefresh struct {
	response searchcore.Response
}

func (refresh peerAnswerRefresh) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return refresh.response, nil
}

type peerAnswerRecent struct{}

func (peerAnswerRecent) Recent(searchcore.Request) (searchcore.Response, bool) {
	return searchcore.Response{}, true
}

func TestPeerAnswerRequiresAnExplicitWireCount(t *testing.T) {
	for _, test := range []struct {
		name     string
		body     string
		answered bool
	}{
		{name: "count zero", body: "count=0\n", answered: true},
		{name: "linkcount zero", body: "linkcount=0\n", answered: true},
		{name: "empty body"},
		{name: "unrelated body", body: "<html>unavailable</html>\n"},
		{name: "count omitted", body: "joincount=0\nsearchtime=1\n"},
		{name: "count empty", body: "count=\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reply, _, err := readRemoteSearchResponseWithinLimit(
				strings.NewReader(test.body), remoteSearchBodyCap,
			)
			if err != nil || reply.response.Count != 0 || len(reply.response.Resources) != 0 ||
				reply.countDeclared != test.answered {
				t.Fatalf("reply = %#v, error = %v", reply, err)
			}
			answering := peerAnswerSeed(t, func(w http.ResponseWriter, _ *http.Request) {
				writeFixtureResponse(t, w, test.body)
			})
			lost := peerAnswerSeed(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			})
			response, err := NewSearcher(Config{
				Peers: fakePeerSource{peers: []yagomodel.Seed{answering, lost}},
			}).Search(t.Context(), searchcore.Request{Terms: []string{"golang"}, Limit: 10})
			if err != nil || len(response.PartialFailures) != 1 ||
				response.PartialFailures[0].Diagnostic.SourceAnswered != test.answered {
				t.Fatalf("response = %#v, error = %v", response, err)
			}
		})
	}
}

func TestPeerAnswerLeavesUnattributedRemoteLossRequired(t *testing.T) {
	response, _ := (searcher{weights: weightsOrDefault(nil)}).responseWithinBudget(
		t.Context(), searchcore.Request{Limit: 10}, []peerSearchResult{
			{wholeQuery: true, countDeclared: true},
			{wholeQuery: true, err: errRemoteSearchTransport},
		}, nil, newRemoteQueryBudget(),
	)
	if len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Source != searchcore.PartialFailureSourceRemoteYaCy ||
		response.PartialFailures[0].Diagnostic.SourceAnswered {
		t.Fatalf("failures = %#v", response.PartialFailures)
	}
}

func TestRetractContestedPeerAnswersIsScopedToThePeer(t *testing.T) {
	peerFailure := func(source string, answered bool) searchcore.PartialFailure {
		return searchcore.PartialFailure{
			Source: source,
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage:          searchcore.FailureStageRemotePeer,
				SourceAnswered: answered,
			},
		}
	}
	failures := []searchcore.PartialFailure{
		peerFailure("contested", true),
		peerFailure("contested", false),
		peerFailure("answered", true),
		{
			Source: searchcore.PartialFailureSourceRemoteStage,
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageRemoteStage,
			},
		},
	}
	retractContestedPeerAnswers(failures, true)
	if failures[0].Diagnostic.SourceAnswered || failures[1].Diagnostic.SourceAnswered ||
		!failures[2].Diagnostic.SourceAnswered || failures[3].Diagnostic.SourceAnswered {
		t.Fatalf("failures = %#v", failures)
	}
}
