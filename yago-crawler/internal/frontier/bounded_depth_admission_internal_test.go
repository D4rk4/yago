package frontier

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestBoundedAdmissionRejectsCandidateBeyondCurrentDepth(t *testing.T) {
	crawlFrontier := NewFrontier(1, nil)
	profile := internalProfile(t)
	profile.Profile.MaxDepth = 1
	runID := uuid.New()
	provenance := []byte("bounded-depth")
	crawlFrontier.state.beginRun(runID, provenance, profile, nil)
	run := crawlFrontier.state.runs[runID]
	run.boundedRecovery = true
	candidate := frontierCandidate{
		normURL:       "https://example.com/deep",
		host:          "example.com",
		depth:         2,
		profileHandle: profile.Profile.Handle,
		provenance:    provenance,
	}
	window := boundedAdmissionWindow{
		visited:  []bool{false},
		pages:    map[string]int{"example.com": 0},
		retired:  make(map[string]struct{}),
		accepted: make(map[string]struct{}),
	}
	accepted, duplicate := crawlFrontier.acceptWithAdmissionWindowLocked(
		context.Background(),
		runID,
		run,
		boundedAdmissionCandidate{page: candidate},
		&window,
	)
	if accepted || duplicate || run.pages != 0 {
		t.Fatalf(
			"deep bounded admission accepted=%t duplicate=%t pages=%d",
			accepted,
			duplicate,
			run.pages,
		)
	}
}
