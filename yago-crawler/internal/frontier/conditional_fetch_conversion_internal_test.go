package frontier

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestFrontierConversionsCarryConditionalFetch(t *testing.T) {
	issued := &yagocrawlcontract.ConditionalFetch{
		Validators: yagocrawlcontract.HTTPValidators{ETag: `"v6"`},
		Revision: yagocrawlcontract.DocumentRevision{
			ContentHash:          "content-hash",
			ExtractionGeneration: 6,
			FetchedAt:            time.Date(2026, 10, 29, 3, 0, 0, 0, time.UTC),
		},
	}
	page := frontiercheckpoint.Page{
		URL:              "https://example.com/cached",
		Host:             "example.com",
		ProfileHandle:    "handle",
		ObservationID:    "observation-cached",
		ConditionalFetch: issued,
	}
	provenance := []byte("conversion-run")
	run := &crawlRun{provenanceValue: provenance}
	restored := checkpointCandidate(page, provenance)
	manifest, _ := seedManifestCandidates(
		frontiercheckpoint.Snapshot{SeedPages: []frontiercheckpoint.Page{page}},
		provenance,
	)
	job := candidateJob(uuid.New(), restored, crawladmission.AdmissionProfile{}, "lease")
	candidates := map[string]frontierCandidate{
		"checkpoint page":     restored,
		"seed manifest":       manifest[0],
		"seed manifest batch": seedManifestPageCandidates([]frontiercheckpoint.Page{page}, provenance)[0],
		"pending page":        pendingCandidate(run, page.Host, pendingPageFromCandidate(restored)),
		"returned job":        candidateFromJob(job),
	}
	if job.ConditionalFetch != issued {
		t.Fatalf("job conditional fetch = %+v, want %+v", job.ConditionalFetch, issued)
	}
	for name, candidate := range candidates {
		if candidate.conditionalFetch != issued {
			t.Fatalf(
				"%s conditional fetch = %+v, want %+v",
				name,
				candidate.conditionalFetch,
				issued,
			)
		}
		if persisted := checkpointPage(candidate).ConditionalFetch; persisted != issued {
			t.Fatalf("%s persisted conditional fetch = %+v, want %+v", name, persisted, issued)
		}
	}
}
