package frontier

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

func boundedLinkSubmission(
	t *testing.T,
	checkpoint *boundedCheckpointScript,
) (*Frontier, crawljob.CrawlJob, frontierCandidate, *crawlRun) {
	t.Helper()
	frontier, runID, run, profile := beginBoundedProducerRun(t, checkpoint)
	frontier.mu.Lock()
	run.leaseID = "submission-lease"
	frontier.mu.Unlock()
	work := crawljob.CrawlJob{
		URL:           "https://recovery.example/source",
		ProfileHandle: profile.Profile.Handle,
		Provenance:    run.provenanceValue,
		LeaseID:       run.leaseID,
		RunID:         runID,
	}
	candidate := preparedCandidate(frontierCandidateSource{
		normalized:    "https://recovery.example/discovered",
		depth:         1,
		profileHandle: profile.Profile.Handle,
		provenance:    run.provenanceValue,
	}, profile)

	return frontier, work, candidate, run
}

func TestDiscoveredLinkSubmissionPropagatesBoundedReadFailure(t *testing.T) {
	readFailure := errors.New("read discovered-link admission")
	checkpoint := &boundedCheckpointScript{admissionStateError: readFailure}
	frontier, work, _, _ := boundedLinkSubmission(t, checkpoint)
	duplicates := frontier.Submit(
		t.Context(),
		work,
		crawljob.DiscoveredLinks{
			Followable: []string{"https://recovery.example/discovered"},
		},
	)
	if duplicates != 0 || !errors.Is(frontier.CheckpointFailure(), readFailure) {
		t.Fatalf(
			"submission duplicates = %d, failure = %v",
			duplicates,
			frontier.CheckpointFailure(),
		)
	}
}

func TestDiscoveredLinkSubmissionDoesNotExtendRecoveryBeforeCommit(t *testing.T) {
	writeFailure := errors.New("write discovered-link admission")
	checkpoint := &boundedCheckpointScript{
		admissionState: frontiercheckpoint.AdmissionBatchState{Visited: []bool{false}},
	}
	checkpoint.admissionError = writeFailure
	frontier, work, candidate, run := boundedLinkSubmission(t, checkpoint)
	duplicates, continued := frontier.submitCandidateBatch(
		t.Context(), work, []frontierCandidate{candidate},
	)
	if duplicates != 0 || continued || !errors.Is(
		frontier.CheckpointFailure(),
		writeFailure,
	) {
		t.Fatalf(
			"failed bounded submission = %d, %t, %v",
			duplicates,
			continued,
			frontier.CheckpointFailure(),
		)
	}
	if run.recoveryUpper != 10 {
		t.Fatalf("failed bounded submission recovery upper = %d", run.recoveryUpper)
	}
}

func TestDiscoveredLinkSubmissionFencesLeaseChangeAfterRead(t *testing.T) {
	checkpoint := &boundedCheckpointScript{
		admissionState: frontiercheckpoint.AdmissionBatchState{Visited: []bool{false}},
	}
	frontier, work, candidate, run := boundedLinkSubmission(t, checkpoint)
	checkpoint.onAdmissionLoad = func() {
		frontier.mu.Lock()
		run.leaseID = "replacement-lease"
		frontier.mu.Unlock()
	}
	duplicates, continued := frontier.submitCandidateBatch(
		t.Context(), work, []frontierCandidate{candidate},
	)
	if duplicates != 0 || continued || frontier.CheckpointFailure() != nil {
		t.Fatalf(
			"lease-fenced submission = %d, %t, %v",
			duplicates,
			continued,
			frontier.CheckpointFailure(),
		)
	}
}

func TestDiscoveredLinkSubmissionRejectsInvalidBoundedState(t *testing.T) {
	frontier, work, candidate, _ := boundedLinkSubmission(
		t,
		&boundedCheckpointScript{},
	)
	duplicates, continued := frontier.submitCandidateBatch(
		t.Context(), work, []frontierCandidate{candidate},
	)
	if duplicates != 0 || continued || !errors.Is(
		frontier.CheckpointFailure(),
		frontiercheckpoint.ErrCorruptCheckpoint,
	) {
		t.Fatalf(
			"invalid bounded submission = %d, %t, %v",
			duplicates,
			continued,
			frontier.CheckpointFailure(),
		)
	}
}

func TestDiscoveredLinkAdmissionIgnoresRemovedRun(t *testing.T) {
	checkpoint := &boundedCheckpointScript{}
	frontier, work, candidate, run := boundedLinkSubmission(t, checkpoint)
	frontier.mu.Lock()
	delete(frontier.state.runs, work.RunID)
	admission, err := frontier.acceptDiscoveredCandidatesLocked(
		t.Context(),
		work,
		run,
		[]frontierCandidate{candidate},
		frontiercheckpoint.AdmissionBatchState{Visited: []bool{false}},
	)
	frontier.mu.Unlock()
	if err != nil || len(admission.accepted) != 0 || admission.duplicates != 0 {
		t.Fatalf("removed-run admission = %+v, %v", admission, err)
	}
}
