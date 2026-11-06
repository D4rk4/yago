package frontiercheckpoint

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type legacyPage struct {
	URL              string
	Host             string
	Depth            int
	ProfileHandle    string
	ObservationID    string
	ObservedAt       time.Time
	SourceModifiedAt time.Time
	RedirectURL      string
	RedirectHost     string
	RedirectHostBump bool
	Index            bool
}

func checkpointConditionalFetch() *yagocrawlcontract.ConditionalFetch {
	return &yagocrawlcontract.ConditionalFetch{
		Validators: yagocrawlcontract.HTTPValidators{
			ETag:         `W/"v7-<&>"`,
			LastModified: "Wed, 28 Oct 2026 02:00:00 GMT",
		},
		Revision: yagocrawlcontract.DocumentRevision{
			ContentHash:          "content-hash",
			ExtractionGeneration: 7,
			FetchedAt:            time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC),
		},
	}
}

func conditionalTestPage() Page {
	page := testPage("https://example.com/cached", "example.com", "cached", 0)
	page.ConditionalFetch = checkpointConditionalFetch()

	return page
}

func TestLegacyCheckpointPageRoundTripsWithoutConditionalFetch(t *testing.T) {
	current := testPage("https://example.com/legacy", "example.com", "legacy", 1)
	legacy := legacyPage{
		URL:              current.URL,
		Host:             current.Host,
		Depth:            current.Depth,
		ProfileHandle:    current.ProfileHandle,
		ObservationID:    current.ObservationID,
		ObservedAt:       current.ObservedAt,
		SourceModifiedAt: current.SourceModifiedAt,
		Index:            current.Index,
	}
	legacyEncoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("encode legacy page: %v", err)
	}
	var decoded Page
	if err := decodeRow("legacy page", legacyEncoded, &decoded); err != nil {
		t.Fatalf("decode legacy page: %v", err)
	}
	requirePageEqual(t, decoded, current)
	currentEncoded, err := encodeRow("page", current)
	if err != nil {
		t.Fatalf("encode current page: %v", err)
	}
	if !bytes.Equal(currentEncoded, legacyEncoded) {
		t.Fatalf("unconditional page encoding changed:\n%s\n%s", currentEncoded, legacyEncoded)
	}
	var legacyDecoded legacyPage
	conditionalEncoded, err := encodeRow("page", conditionalTestPage())
	if err != nil {
		t.Fatalf("encode conditional page: %v", err)
	}
	if err := json.Unmarshal(conditionalEncoded, &legacyDecoded); err != nil {
		t.Fatalf("legacy crawler refused conditional page: %v", err)
	}
	if legacyDecoded.URL != "https://example.com/cached" {
		t.Fatalf("legacy decoded page = %+v", legacyDecoded)
	}
}

func TestConditionalFetchPageSurvivesCheckpointReopen(t *testing.T) {
	path := testCheckpointPath(t)
	provenance := []byte("conditional-reopen")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}
	beginSeedManifest(t, first, provenance, []Page{conditionalTestPage()})
	if err := first.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}

	reopened := openTestCheckpoint(t, path)
	snapshot, err := reopened.Load(testContext, provenance)
	if err != nil || len(snapshot.SeedPages) != 1 {
		t.Fatalf("reopened snapshot = %+v, %v", snapshot, err)
	}
	restored := snapshot.SeedPages[0].ConditionalFetch
	want := checkpointConditionalFetch()
	if restored == nil || restored.Validators != want.Validators ||
		!restored.Revision.Matches(want.Revision) {
		t.Fatalf("restored conditional fetch = %+v, want %+v", restored, want)
	}
}

func TestSeedManifestRefusesChangedConditionalFetch(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("conditional-manifest")
	beginSeedManifest(t, checkpoint, provenance, []Page{conditionalTestPage()})
	changedValidators := conditionalTestPage()
	changedValidators.ConditionalFetch.Validators.ETag = `"v8"`
	changedRevision := conditionalTestPage()
	changedRevision.ConditionalFetch.Revision.ExtractionGeneration = 8
	withoutConditionalFetch := conditionalTestPage()
	withoutConditionalFetch.ConditionalFetch = nil
	for name, page := range map[string]Page{
		"changed validators":        changedValidators,
		"changed revision":          changedRevision,
		"without conditional fetch": withoutConditionalFetch,
	} {
		_, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
			Decisions: []SeedDecision{{Page: page, Admit: true}},
		})
		if !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("%s seed decision error = %v, want ErrCorruptCheckpoint", name, err)
		}
	}
	sameInstant := conditionalTestPage()
	sameInstant.ConditionalFetch.Revision.FetchedAt = sameInstant.ConditionalFetch.Revision.FetchedAt.In(
		time.FixedZone("test", 7200),
	)

	result, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
		Decisions: []SeedDecision{{Page: sameInstant, Admit: true}},
	})

	if err != nil || result.Admitted != 1 {
		t.Fatalf("unchanged seed decision = %+v, %v", result, err)
	}
}
