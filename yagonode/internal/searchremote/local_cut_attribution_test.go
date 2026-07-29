package searchremote

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// TestPeerFailureBlamesTheLocalStageForThisNodesOwnCuts pins which side of a cut
// call gets named. This node ends a peer call itself when its outbound response
// budget runs out or its admission gate is cancelled, sometimes before the peer
// is reached at all. Stamping the peer's hash on that failure made
// peerSearchFailureTotal count a healthy peer as failing, and the console blame
// it, whenever this node was busy. recordPeerFailure already kept these out of
// the peer's reputation; the partial failure now draws the same line.
func TestPeerFailureBlamesTheLocalStageForThisNodesOwnCuts(t *testing.T) {
	t.Parallel()

	peer := yagomodel.Seed{Hash: yagomodel.WordHash("peer")}
	for _, cause := range []error{
		errRemoteSearchBudgetExhausted,
		errRemoteSearchAdmissionCanceled,
	} {
		failure := peerFailure(peer, cause)
		if failure.Source != searchcore.PartialFailureSourceRemoteStage {
			t.Fatalf("source for %v = %q, want the local remote stage", cause, failure.Source)
		}
		if failure.Reason != cause.Error() {
			t.Fatalf("reason for %v = %q", cause, failure.Reason)
		}
	}

	// The other half of the guard: a failure the peer really is responsible for
	// must still name the peer, or the reputation and console lose their subject.
	refused := peerFailure(peer, errors.New("connection refused"))
	if refused.Source != peer.Hash.String() {
		t.Fatalf("peer error source = %q, want the peer hash %q", refused.Source, peer.Hash)
	}
	if anonymous := peerFailure(
		yagomodel.Seed{},
		errors.New("connection refused"),
	); anonymous.Source != searchcore.PartialFailureSourceRemoteYaCy {
		t.Fatalf("hashless peer source = %q", anonymous.Source)
	}
}

// TestTermlessFanOutIsDecidedOnWordHashesNotTermCount pins the guard to the
// thing that actually decides whether the DHT can be asked. termHashes discards
// blank terms, so a request carrying only whitespace has terms but no word
// hashes; a guard written against len(req.Terms) would wave it through and the
// fan-out would report a lost peer for a query no peer was ever sent.
func TestTermlessFanOutIsDecidedOnWordHashesNotTermCount(t *testing.T) {
	t.Parallel()

	resp := searcher{}.searchExact(
		t.Context(),
		searchcore.Request{Terms: []string{"   ", ""}, Limit: 10},
		queryMatchEvidenceBinding{},
		nil,
		newRemoteQueryBudget(),
	)
	if len(resp.PartialFailures) != 1 ||
		resp.PartialFailures[0].Source != searchcore.PartialFailureSourceQueryShape {
		t.Fatalf("failures = %#v, want a single query-shape note", resp.PartialFailures)
	}

	// The over-refusal guard: a request that does yield a hash must reach the
	// peer source, whose absence is a genuine loss of coverage.
	real := searcher{}.searchExact(
		t.Context(),
		searchcore.Request{Terms: []string{"golang"}, Limit: 10},
		queryMatchEvidenceBinding{},
		nil,
		newRemoteQueryBudget(),
	)
	if len(real.PartialFailures) != 1 ||
		real.PartialFailures[0].Source != searchcore.PartialFailureSourceRemoteYaCy {
		t.Fatalf(
			"failures = %#v, want the missing peer source reported as a loss",
			real.PartialFailures,
		)
	}
}

// TestSearchVariantsReportsThePassesItSkipped pins the v0.0.26 limitation that a
// morphology fan-out cut by the swarm deadline reported nothing at all: the
// unrun passes produced neither a result nor a failure, so the truncated fusion
// was indistinguishable from a complete one. An empty one then satisfied
// UnprovenZero as a truthful zero, and the session layer could store it as a
// settled result set.
func TestSearchVariantsReportsThePassesItSkipped(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	resp := searcher{}.searchVariants(
		canceled,
		searchcore.Request{Terms: []string{"run"}, Limit: 10},
		[]string{"run", "runs", "running"},
		nil,
		newRemoteQueryBudget(),
	)
	if len(resp.Results) != 0 {
		t.Fatalf("results = %#v, want none from a cut fan-out", resp.Results)
	}
	if len(resp.PartialFailures) != 1 {
		t.Fatalf("failures = %#v, want the skipped passes reported", resp.PartialFailures)
	}
	failure := resp.PartialFailures[0]
	if failure.Source != searchcore.PartialFailureSourceRemoteStage {
		t.Fatalf("source = %q, want the local remote stage", failure.Source)
	}
	if !strings.Contains(failure.Reason, "3 query variant(s) skipped") {
		t.Fatalf("reason = %q, want the three unrun passes counted", failure.Reason)
	}
}

// TestSearchVariantsReportsNoSkipWhenEveryPassRan is the over-reporting guard:
// a fan-out that ran every pass must not claim one was skipped, or every
// morphology search would label itself incomplete.
func TestSearchVariantsReportsNoSkipWhenEveryPassRan(t *testing.T) {
	t.Parallel()

	resp := searcher{}.searchVariants(
		t.Context(),
		searchcore.Request{Terms: []string{"run"}, Limit: 10},
		[]string{"run", "runs"},
		nil,
		newRemoteQueryBudget(),
	)
	for _, failure := range resp.PartialFailures {
		if strings.Contains(failure.Reason, "skipped before completion") {
			t.Fatalf("no pass was skipped, yet one was reported: %#v", failure)
		}
	}
}
