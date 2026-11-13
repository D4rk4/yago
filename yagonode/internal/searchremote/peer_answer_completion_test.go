package searchremote

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
	"github.com/D4rk4/yago/yagoproto"
)

func TestRemoteSearchRequiresCompletedAnswersAcrossAcceptedVariants(t *testing.T) {
	for _, reply := range []struct {
		name     string
		body     string
		count    int
		answered int
	}{
		{name: "blank"},
		{name: "count omitted", body: "joincount=0\nsearchtime=1\n"},
		{name: "declared resources missing", body: "count=1\n", count: 1},
		{name: "complete empty", body: "count=0\n", answered: 1},
	} {
		t.Run(reply.name, func(t *testing.T) {
			parsed, _, err := readRemoteSearchResponseWithinLimit(
				strings.NewReader(reply.body), remoteSearchBodyCap,
			)
			if err != nil || parsed.response.Count != reply.count ||
				len(parsed.response.Resources) != 0 {
				t.Fatalf("legacy accepted reply = %#v, error = %v", parsed, err)
			}
			for _, lossVariant := range []string{"instrument", "instruments"} {
				t.Run(lossVariant, func(t *testing.T) {
					assertAcceptedVariantAnswers(t, reply.body, lossVariant, reply.answered)
				})
			}
		})
	}
}

func assertAcceptedVariantAnswers(t *testing.T, body, lossVariant string, answered int) {
	t.Helper()
	var calls atomic.Int32
	peers := make([]yagomodel.Seed, 0, 2)
	lossQuery := yagomodel.WordHash(lossVariant).String()
	for _, unavailable := range []bool{false, true} {
		peers = append(peers, peerAnswerSeed(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.URL.Query().Get(yagoproto.FieldQuery) == lossQuery {
				if unavailable {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				writeFixtureResponse(t, w, "count=0\n")
				return
			}
			writeFixtureResponse(t, w, body)
		}))
	}
	response, err := NewSearcher(Config{
		Peers:      fakePeerSource{peers: peers},
		ExpandWord: func(word string) []string { return []string{word, word + "s"} },
	}).Search(t.Context(), searchcore.Request{Terms: []string{"instrument"}, Limit: 10})
	if err != nil || calls.Load() != 4 || len(response.Results) != 0 ||
		len(response.PartialFailures) != 1 || response.LostSourceFailures() != 1 {
		t.Fatalf("response = %#v, calls = %d, error = %v", response, calls.Load(), err)
	}
	failure := response.PartialFailures[0]
	if failure.Source != peers[1].Hash.String() ||
		failure.Reason != "remote search failed: status 502" ||
		failure.Diagnostic.Stage != searchcore.FailureStageRemotePeer ||
		failure.Diagnostic.Cause != searchcore.FailureCauseBackend {
		t.Fatalf("legacy diagnostic changed: %#v", failure)
	}
	if answeredPeerFailures(response.PartialFailures) != answered {
		t.Errorf("answered failures = %#v, want %d", response.PartialFailures, answered)
	}
	assertVariantPeerAnswersSurviveSessionMerge(t, response, answered)
}

func TestRemoteSearchRequiresLiveOverallStageForAnsweredPeerTimeout(t *testing.T) {
	for _, test := range []struct {
		name           string
		perPeerTimeout time.Duration
		overallTimeout time.Duration
		answered       int
	}{
		{name: "overall deadline", perPeerTimeout: 2 * time.Second, overallTimeout: time.Second},
		{name: "peer deadline", perPeerTimeout: time.Second, overallTimeout: 2 * time.Second, answered: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				assertPeerAnswerDeadline(t, test.perPeerTimeout, test.overallTimeout, test.answered)
			})
		})
	}
}

func assertPeerAnswerDeadline(
	t *testing.T,
	perPeerTimeout, overallTimeout time.Duration,
	answered int,
) {
	t.Helper()
	answering := serverSeed(t, "http://192.0.2.1:8101")
	pending := serverSeed(t, "http://192.0.2.1:8102")
	observations := &capturedReputationObservations{}
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Port() == "8102" {
				<-request.Context().Done()
				return nil, request.Context().Err()
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("count=0\n")),
			}, nil
		}),
	}
	response, err := NewSearcher(Config{
		Client:                 client,
		Peers:                  fakePeerSource{peers: []yagomodel.Seed{answering, pending}},
		PerPeerTimeout:         perPeerTimeout,
		OverallTimeout:         overallTimeout,
		ReputationObservations: observations,
	}).Search(t.Context(), searchcore.Request{Terms: []string{"instrument"}, Limit: 10})
	if err != nil || len(response.Results) != 0 || len(response.PartialFailures) != 1 ||
		response.LostSourceFailures() != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	failure := response.PartialFailures[0]
	if failure.Source != pending.Hash.String() ||
		!strings.Contains(failure.Reason, context.DeadlineExceeded.Error()) ||
		failure.Diagnostic.Stage != searchcore.FailureStageRemotePeer ||
		failure.Diagnostic.Cause != searchcore.FailureCauseDeadline {
		t.Fatalf("legacy diagnostic changed: %#v", failure)
	}
	assertPeerDeadlineReputation(t, observations, answering, pending)
	if answeredPeerFailures(response.PartialFailures) != answered {
		t.Errorf("answered failures = %#v, want %d", response.PartialFailures, answered)
	}
	assertVariantPeerAnswersSurviveSessionMerge(t, response, answered)
}

func TestRemoteSearchCompletedEmptyVariantsRemainExhaustive(t *testing.T) {
	var calls atomic.Int32
	peer := peerAnswerSeed(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeFixtureResponse(t, w, "count=0\n")
	})
	remote := NewSearcher(Config{
		Peers:      fakePeerSource{peers: []yagomodel.Seed{peer}},
		ExpandWord: func(word string) []string { return []string{word, word + "s"} },
	})
	response, err := searchsession.WithStableWindow(remote).Search(t.Context(), searchcore.Request{
		Query: "instrument", Terms: []string{"instrument"}, Limit: 10,
	})
	if err != nil || calls.Load() != 2 || len(response.Results) != 0 ||
		len(response.PartialFailures) != 0 || response.LostSourceFailures() != 0 ||
		response.UnprovenZero() || !response.Availability.Exhausted {
		t.Fatalf("response = %#v, calls = %d, error = %v", response, calls.Load(), err)
	}
}

func assertPeerDeadlineReputation(
	t *testing.T,
	observations *capturedReputationObservations,
	answering yagomodel.Seed,
	pending yagomodel.Seed,
) {
	t.Helper()
	if len(observations.batches) != 1 || len(observations.batches[0]) != 2 {
		t.Fatalf("observations = %#v", observations.batches)
	}
	for _, observation := range observations.batches[0] {
		switch observation.Peer {
		case peerreputation.SignedPeerIdentity(answering.Hash.String()):
			if observation.Outcome != peerreputation.OutcomeSuccess {
				t.Fatalf("answer reputation = %#v", observation)
			}
		case peerreputation.SignedPeerIdentity(pending.Hash.String()):
			if observation.Outcome != peerreputation.OutcomeTimeout {
				t.Fatalf("deadline reputation = %#v", observation)
			}
		default:
			t.Fatalf("unexpected observation = %#v", observation)
		}
	}
}
