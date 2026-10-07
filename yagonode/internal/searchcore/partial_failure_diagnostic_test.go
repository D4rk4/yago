package searchcore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestPartialFailureDiagnosticDoesNotChangeWireFields(t *testing.T) {
	encoded, err := json.Marshal(PartialFailure{
		Source: "remote-yacy",
		Reason: "source failed",
		Diagnostic: PartialFailureDiagnostic{
			Stage: FailureStageRemotePeer,
			Cause: FailureCauseDeadline,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"source":"remote-yacy","reason":"source failed"}` {
		t.Fatalf("partial failure wire fields = %s", encoded)
	}
}

func TestFailureDiagnosticCountsOnlyAllowlistedTypedMetadata(t *testing.T) {
	got := FailureDiagnosticCounts([]PartialFailure{
		{
			Source: "PEER_HASH_CANARY_A",
			Reason: "PRIVATE_QUERY_CANARY PRIVATE_URL_CANARY PRIVATE_ERROR_CANARY",
			Diagnostic: PartialFailureDiagnostic{
				Stage: FailureStageRemotePeer,
				Cause: FailureCauseDeadline,
			},
		},
		{
			Source: "PEER_HASH_CANARY_B",
			Reason: "PRIVATE_ERROR_CANARY",
			Diagnostic: PartialFailureDiagnostic{
				Stage: FailureStageRemotePeer,
				Cause: FailureCauseDeadline,
			},
		},
		{
			Source: "unrecognized source",
			Reason: "unrecognized reason",
			Diagnostic: PartialFailureDiagnostic{
				Stage: FailureStage(255),
				Cause: FailureCause(255),
			},
		},
		{
			Source: PartialFailureSourceWeb,
			Diagnostic: PartialFailureDiagnostic{
				Stage: FailureStageWebSearch,
				Cause: FailureCauseUnavailable,
			},
		},
		{
			Source: PartialFailureSourceWeb,
			Diagnostic: PartialFailureDiagnostic{
				Stage: FailureStageWebSearch,
				Cause: FailureCauseUnsupported,
			},
		},
		{
			Source: PartialFailureSourceQueryShape,
			Reason: "query shape",
			Diagnostic: PartialFailureDiagnostic{
				Stage: FailureStageQueryShape,
				Cause: FailureCauseNoTarget,
			},
		},
	})
	want := []FailureDiagnosticCount{
		{Stage: "other", Cause: "other", Count: 1},
		{Stage: "remote-peer", Cause: "deadline", Count: 2},
		{Stage: "web", Cause: "unavailable", Count: 1},
		{Stage: "web", Cause: "unsupported", Count: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("diagnostic counts = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("diagnostic counts = %#v, want %#v", got, want)
		}
	}
}

func TestFailureDiagnosticCountsKeepExactBudgetCount(t *testing.T) {
	failures := make([]PartialFailure, 300)
	for index := range failures {
		failures[index].Diagnostic = PartialFailureDiagnostic{
			Stage: FailureStageRemotePeer,
			Cause: FailureCauseBudget,
		}
	}
	got := FailureDiagnosticCounts(failures)
	want := []FailureDiagnosticCount{{
		Stage: "remote-peer",
		Cause: "budget",
		Count: len(failures),
	}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("diagnostic counts = %#v, want %#v", got, want)
	}
}

func TestFailureDiagnosticLabelsRemainBounded(t *testing.T) {
	stageLabels := []struct {
		stage FailureStage
		want  string
	}{
		{FailureStageUnknown, "other"},
		{FailureStageLocalSearch, PartialFailureSourceLocalSearch},
		{FailureStageRemoteSearch, PartialFailureSourceRemoteYaCy},
		{FailureStageRemotePeer, "remote-peer"},
		{FailureStageRemoteStage, PartialFailureSourceRemoteStage},
		{FailureStagePeerReputation, PartialFailureSourcePeerReputation},
		{FailureStageExactSearch, PartialFailureSourceExactStage},
		{FailureStageLocalExactSearch, PartialFailureSourceLocalExactStage},
		{FailureStageFuzzySearch, PartialFailureSourceFuzzyStage},
		{FailureStageLocalEvidence, PartialFailureSourceLocalEvidence},
		{FailureStageWebSearch, "web"},
		{FailureStageQueryShape, "other"},
		{FailureStage(255), "other"},
	}
	for _, test := range stageLabels {
		if got := failureStageLabel(test.stage); got != test.want {
			t.Errorf("failure stage label %d = %q, want %q", test.stage, got, test.want)
		}
	}
	causeLabels := []struct {
		cause FailureCause
		want  string
	}{
		{FailureCauseUnknown, "other"},
		{FailureCauseNoTarget, "no-target"},
		{FailureCauseDeadline, "deadline"},
		{FailureCauseCapacity, "capacity"},
		{FailureCauseBudget, "budget"},
		{FailureCauseUnavailable, "unavailable"},
		{FailureCauseBackend, "backend"},
		{FailureCauseCanceled, "canceled"},
		{FailureCauseUnsupported, "unsupported"},
		{FailureCause(255), "other"},
	}
	for _, test := range causeLabels {
		if got := FailureCauseLabel(test.cause); got != test.want {
			t.Errorf("failure cause label %d = %q, want %q", test.cause, got, test.want)
		}
	}
	for _, test := range []struct {
		source string
		want   FailureStage
	}{
		{PartialFailureSourceLocalSearch, FailureStageLocalSearch},
		{PartialFailureSourceRemoteYaCy, FailureStageRemoteSearch},
		{PartialFailureSourceRemoteStage, FailureStageRemoteStage},
		{PartialFailureSourcePeerReputation, FailureStagePeerReputation},
		{PartialFailureSourceExactStage, FailureStageExactSearch},
		{PartialFailureSourceLocalExactStage, FailureStageLocalExactSearch},
		{PartialFailureSourceFuzzyStage, FailureStageFuzzySearch},
		{PartialFailureSourceLocalEvidence, FailureStageLocalEvidence},
		{PartialFailureSourceWeb, FailureStageWebSearch},
		{PartialFailureSourceQueryShape, FailureStageQueryShape},
		{"unknown", FailureStageUnknown},
	} {
		if got := FailureStageForSource(test.source); got != test.want {
			t.Errorf("failure stage for %q = %d, want %d", test.source, got, test.want)
		}
	}
	for _, test := range []struct {
		err  error
		want FailureCause
	}{
		{nil, FailureCauseUnknown},
		{context.DeadlineExceeded, FailureCauseDeadline},
		{context.Canceled, FailureCauseCanceled},
		{errors.New("backend failure"), FailureCauseBackend},
	} {
		if got := FailureCauseFor(test.err); got != test.want {
			t.Errorf("failure cause for %v = %d, want %d", test.err, got, test.want)
		}
	}
}
