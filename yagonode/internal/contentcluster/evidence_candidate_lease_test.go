package contentcluster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type candidateReplacementResult struct {
	replacements []EvidenceReplacement
	err          error
}

type candidatePlanningCase struct {
	name       string
	first      Evidence
	second     Evidence
	gateBucket vault.Name
	gateKey    vault.Key
}

func TestConcurrentCandidatePlanningCoalescesClusters(t *testing.T) {
	limits := DefaultLimits()
	limits.ShingleWords = 2
	limits.MinimumJaccard = 0.75
	nearText := "alpha beta gamma delta epsilon zeta eta theta"
	nearPrepared, err := prepareEvidence(t.Context(), limits, Evidence{
		URL:         "https://near-first.example/page",
		ContentHash: "near-first",
		Text:        nearText,
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []candidatePlanningCase{
		{
			name: "exact",
			first: Evidence{
				URL:         "https://exact-first.example/page",
				ContentHash: "shared-exact",
				Text:        "one two three four five six",
			},
			second: Evidence{
				URL:         "https://exact-second.example/page",
				ContentHash: "shared-exact",
				Text:        "unrelated text still shares exact identity",
			},
			gateBucket: exactBucketName,
			gateKey:    vault.Key("shared-exact"),
		},
		{
			name: "near",
			first: Evidence{
				URL:         "https://near-first.example/page",
				ContentHash: "near-first",
				Text:        nearText,
			},
			second: Evidence{
				URL:         "https://near-second.example/page",
				ContentHash: "near-second",
				Text:        nearText,
			},
			gateBucket: bandBucketName,
			gateKey:    bandKey(0, nearPrepared.Bands[0]),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertConcurrentCandidatesCoalesce(t, limits, test)
		})
	}
}

func assertConcurrentCandidatesCoalesce(
	t *testing.T,
	limits Limits,
	test candidatePlanningCase,
) {
	t.Helper()
	index, engine := openFaultIndex(t, limits)
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	engine.readGateBucket = test.gateBucket
	engine.readGateKey = string(test.gateKey)
	engine.readGateEntered = entered
	engine.readGateRelease = release
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	firstResult := make(chan candidateReplacementResult, 1)
	secondResult := make(chan candidateReplacementResult, 1)
	go replaceCandidateContext(ctx, index, test.first, firstResult)
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("first candidate did not reach planning")
	}
	go replaceCandidateContext(ctx, index, test.second, secondResult)
	crossedPlanning := false
	select {
	case <-entered:
		crossedPlanning = true
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	firstOutcome := receiveCandidateReplacement(t, firstResult)
	completedBeforeFinalization := false
	var secondOutcome candidateReplacementResult
	select {
	case secondOutcome = <-secondResult:
		completedBeforeFinalization = true
	case <-time.After(25 * time.Millisecond):
	}
	if firstOutcome.err == nil {
		if err := index.FinalizeEvidenceTransitions(
			t.Context(),
			replacementFinalizations(firstOutcome.replacements),
		); err != nil {
			t.Fatal(err)
		}
	}
	if !completedBeforeFinalization {
		secondOutcome = receiveCandidateReplacement(t, secondResult)
	}
	if secondOutcome.err == nil {
		if err := index.FinalizeEvidenceTransitions(
			t.Context(),
			replacementFinalizations(secondOutcome.replacements),
		); err != nil {
			t.Fatal(err)
		}
	}
	if firstOutcome.err != nil || secondOutcome.err != nil {
		t.Fatalf("candidate replacements failed: %v / %v", firstOutcome.err, secondOutcome.err)
	}
	if crossedPlanning {
		t.Error("competing candidate crossed the planning fence")
	}
	if completedBeforeFinalization {
		t.Error("competing candidate crossed external finalization")
	}
	if firstOutcome.replacements[0].Current.ClusterID !=
		secondOutcome.replacements[0].Current.ClusterID {
		t.Errorf(
			"concurrent candidates formed clusters %q and %q",
			firstOutcome.replacements[0].Current.ClusterID,
			secondOutcome.replacements[0].Current.ClusterID,
		)
	}
}

func receiveCandidateReplacement(
	t *testing.T,
	completed <-chan candidateReplacementResult,
) candidateReplacementResult {
	t.Helper()
	select {
	case result := <-completed:
		return result
	case <-time.After(time.Second):
		t.Fatal("candidate replacement did not complete")

		return candidateReplacementResult{}
	}
}

func TestCandidateLeaseWaitHonorsCancellation(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	first, err := index.ReplaceBatch(t.Context(), []Evidence{{
		URL:         "https://candidate-held.example/page",
		ContentHash: "candidate-held",
		Text:        "one two three four",
	}})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	completed := make(chan error, 1)
	go func() {
		_, err := index.Replace(cancelled, Evidence{
			URL:         "https://candidate-waiter.example/page",
			ContentHash: "candidate-held",
			Text:        "different text with exact identity",
		})
		completed <- err
	}()
	select {
	case err := <-completed:
		t.Fatalf("candidate wait returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("candidate wait error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled candidate wait did not return")
	}
	index.ReleaseEvidenceTransitions(replacementFinalizations(first))
}

func TestPreparedReplacementURLWaitHonorsCancellation(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	url := "https://prepared-url-held.example/page"
	first, err := index.ReplaceBatch(t.Context(), []Evidence{{
		URL:         url,
		ContentHash: "prepared-url-held",
		Text:        "one two three four",
	}})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	completed := make(chan error, 1)
	go func() {
		_, err := index.Replace(cancelled, Evidence{
			URL:         url,
			ContentHash: "prepared-url-waiter",
			Text:        "five six seven eight",
		})
		completed <- err
	}()
	select {
	case err := <-completed:
		t.Fatalf("URL wait returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("URL wait error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled URL wait did not return")
	}
	index.ReleaseEvidenceTransitions(replacementFinalizations(first))
}

func TestCandidatePrecedesExistingClusterProjection(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	shared := Evidence{
		URL:         "https://projection-member.example/page",
		ContentHash: "projection-candidate",
		Text:        "one two three four five six",
	}
	cluster := replaceEvidence(t, index, shared)
	existing := shared
	existing.URL = "https://projection-existing.example/page"
	assignment := replaceEvidence(t, index, existing)
	if assignment.ClusterID != cluster.ClusterID {
		t.Fatalf("existing cluster = %q, want %q", assignment.ClusterID, cluster.ClusterID)
	}
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	engine.readGateBucket = exactBucketName
	engine.readGateKey = shared.ContentHash
	engine.readGateEntered = entered
	engine.readGateRelease = release
	competing := shared
	competing.URL = "https://projection-competing.example/page"
	existing.Quality = 2
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	competingResult := make(chan candidateReplacementResult, 1)
	existingResult := make(chan candidateReplacementResult, 1)
	go replaceCandidateContext(ctx, index, competing, competingResult)
	select {
	case <-entered:
	case <-ctx.Done():
		close(release)
		t.Fatal("competing candidate did not reach planning")
	}
	go replaceCandidateContext(ctx, index, existing, existingResult)
	select {
	case outcome := <-existingResult:
		close(release)
		t.Fatalf("existing replacement crossed the candidate lease: %v", outcome.err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	competingOutcome := receiveCandidateReplacement(t, competingResult)
	if competingOutcome.err != nil {
		t.Fatal(competingOutcome.err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(competingOutcome.replacements),
	); err != nil {
		t.Fatal(err)
	}
	existingOutcome := receiveCandidateReplacement(t, existingResult)
	if existingOutcome.err != nil {
		t.Fatal(existingOutcome.err)
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(existingOutcome.replacements),
	); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []candidateReplacementResult{competingOutcome, existingOutcome} {
		if outcome.replacements[0].Current.ClusterID != cluster.ClusterID {
			t.Fatalf(
				"replacement cluster = %q, want %q",
				outcome.replacements[0].Current.ClusterID,
				cluster.ClusterID,
			)
		}
	}
}

func TestInitialProjectionLeaseWaitHonorsCancellation(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	evidence := Evidence{
		URL:         "https://initial-projection.example/page",
		ContentHash: "initial-projection",
		Text:        "one two three four",
	}
	assignment := replaceEvidence(t, index, evidence)
	held, err := index.projections.acquireLease(
		t.Context(),
		[]string{assignment.ClusterID},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer held.close()
	prepared, err := prepareEvidence(t.Context(), index.limits, Evidence{
		URL:         evidence.URL,
		ContentHash: "changed-candidate",
		Text:        "five six seven eight",
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		acquire func(context.Context) error
	}{
		{
			name: "replacement",
			acquire: func(ctx context.Context) error {
				leases, err := index.acquirePreparedReplacementLeases(
					ctx,
					[]string{evidence.URL},
					[]preparedEvidence{prepared},
				)
				if leases != nil {
					leases.close()
				}

				return err
			},
		},
		{
			name: "deletion",
			acquire: func(ctx context.Context) error {
				leases, err := index.acquireReplacementLeases(ctx, []string{evidence.URL})
				if leases != nil {
					leases.close()
				}

				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			completed := make(chan error, 1)
			go func() { completed <- test.acquire(ctx) }()
			select {
			case err := <-completed:
				t.Fatalf("projection wait returned before cancellation: %v", err)
			case <-time.After(25 * time.Millisecond):
			}
			cancel()
			select {
			case err := <-completed:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("projection wait error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled projection wait did not return")
			}
		})
	}
}

func TestReleasedCandidateLeaseAllowsTransitionReplay(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	evidence := Evidence{
		URL:         "https://candidate-replay.example/page",
		ContentHash: "candidate-replay",
		Text:        "one two three four",
	}
	first, err := index.ReplaceBatch(t.Context(), []Evidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	index.ReleaseEvidenceTransitions(replacementFinalizations(first))
	replayed, err := index.ReplaceBatch(t.Context(), []Evidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed[0].Replay {
		t.Fatal("pending transition did not replay")
	}
	if err := index.FinalizeEvidenceTransitions(
		t.Context(),
		replacementFinalizations(replayed),
	); err != nil {
		t.Fatal(err)
	}
	joined := replaceEvidence(t, index, Evidence{
		URL:         "https://candidate-replay-copy.example/page",
		ContentHash: evidence.ContentHash,
		Text:        "unrelated text with exact identity",
	})
	if joined.ClusterID != replayed[0].Current.ClusterID {
		t.Fatalf(
			"replayed cluster = %q, joined cluster = %q",
			replayed[0].Current.ClusterID,
			joined.ClusterID,
		)
	}
}

func replaceCandidateContext(
	ctx context.Context,
	index *Index,
	evidence Evidence,
	completed chan<- candidateReplacementResult,
) {
	replacements, err := index.ReplaceBatch(ctx, []Evidence{evidence})
	completed <- candidateReplacementResult{replacements: replacements, err: err}
}
