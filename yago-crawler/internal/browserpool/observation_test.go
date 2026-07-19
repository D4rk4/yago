package browserpool

import (
	"slices"
	"testing"
)

func TestFailureReasonsAreFixedAndRecognized(t *testing.T) {
	want := []FailureReason{
		FailureSlotDeadline,
		FailureCooldown,
		FailureLaunch,
		FailureRender,
	}
	reasons := FailureReasons()
	if !slices.Equal(reasons[:], want) {
		t.Fatalf("failure reasons = %v, want %v", reasons, want)
	}
	for _, reason := range reasons {
		if !reason.Recognized() {
			t.Fatalf("failure reason %q was not recognized", reason)
		}
	}
	if FailureReason("dynamic").Recognized() {
		t.Fatal("dynamic failure reason was recognized")
	}
	reasons[0] = "changed"
	if FailureReasons()[0] != FailureSlotDeadline {
		t.Fatal("failure reason vocabulary was mutable")
	}
}

func TestSessionStatesAreFixed(t *testing.T) {
	want := []SessionState{SessionReady, SessionBusy, SessionCooling}
	states := SessionStates()
	if !slices.Equal(states[:], want) {
		t.Fatalf("session states = %v, want %v", states, want)
	}
	states[0] = "changed"
	if SessionStates()[0] != SessionReady {
		t.Fatal("session state vocabulary was mutable")
	}
}
