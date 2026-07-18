package frontier

import (
	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
)

func (s *frontierState) beginRun(
	runID uuid.UUID,
	provenance []byte,
	profile crawladmission.AdmissionProfile,
	finish func(succeeded bool),
) {
	s.runs[runID] = &crawlRun{
		visited:                make(map[string]struct{}),
		hostPages:              make(map[string]int),
		pendingByHost:          make(map[string]*pendingHostPages),
		hostFailures:           make(map[string]uint8),
		hostGenerations:        make(map[string]uint64),
		retiredHosts:           make(map[string]struct{}),
		residentHostReferences: make(map[string]int),
		redirects:              make(map[string]redirectReservation),
		pageHostProgress:       make(map[string]stagedPageHostProgress),
		profiles: map[string]crawladmission.AdmissionProfile{
			profile.Profile.Handle: profile,
		},
		provenance:      string(provenance),
		provenanceValue: provenance,
		seeding:         true,
		maxPages:        profile.Profile.EffectiveMaxPagesPerRun(s.maxPagesPerRun),
	}
	s.completion.Begin(runID, finish)
}
