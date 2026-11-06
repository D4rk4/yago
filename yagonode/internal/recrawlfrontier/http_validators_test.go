package recrawlfrontier

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
)

const validatedPageURL = "https://a.example/validated"

func storedRevision() yagocrawlcontract.DocumentRevision {
	return yagocrawlcontract.DocumentRevision{
		ContentHash:          "sha256:stored",
		ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration,
		FetchedAt:            testBase,
	}
}

func usableBinding() ValidatorBinding {
	return ValidatorBinding{
		DocumentURL: validatedPageURL,
		Validators: yagocrawlcontract.HTTPValidators{
			ETag:         `"first"`,
			LastModified: "Sat, 04 Jul 2026 11:00:00 GMT",
		},
		Revision:   storedRevision(),
		ObservedAt: testBase,
	}
}

func unchangedPage(observedAt time.Time) UnchangedPage {
	return UnchangedPage{
		URL:           validatedPageURL,
		ProfileHandle: "handle-a",
		Revision:      storedRevision(),
		ObservedAt:    observedAt,
	}
}

func boundFrontier(t *testing.T) (*Frontier, *ctrlEngine) {
	t.Helper()
	frontier, engine := openCtrlFrontier(t)
	mustObserve(t, frontier, observation{
		url: validatedPageURL, handle: "handle-a", interval: time.Hour, at: testBase,
	})
	if err := frontier.BindHTTPValidators(
		context.Background(),
		validatedPageURL,
		usableBinding(),
	); err != nil {
		t.Fatalf("bind validators: %v", err)
	}

	return frontier, engine
}

func storedScheduleBytes(t *testing.T, engine *ctrlEngine) []byte {
	t.Helper()

	return bytes.Clone(engine.buckets[recordBucket][hashKey(t, validatedPageURL)])
}

func mustBinding(t *testing.T, frontier *Frontier) (ValidatorBinding, bool) {
	t.Helper()
	binding, found, err := frontier.ValidatorBinding(context.Background(), validatedPageURL)
	if err != nil {
		t.Fatalf("read validator binding: %v", err)
	}

	return binding, found
}

func mustRecordUnchanged(t *testing.T, frontier *Frontier, page UnchangedPage) bool {
	t.Helper()
	recorded, err := frontier.RecordUnchangedPage(context.Background(), page)
	if err != nil {
		t.Fatalf("record unchanged page: %v", err)
	}

	return recorded
}

type scheduleRecordBeforeValidators struct {
	URL              string
	ProfileHandle    string
	Interval         time.Duration
	NextDueAt        time.Time
	SourceModifiedAt time.Time
}

func TestScheduleRecordWithoutValidatorsEncodesExactlyAsBefore(t *testing.T) {
	record := scheduleRecord{
		URL: validatedPageURL, ProfileHandle: "handle-a", Interval: time.Hour,
		NextDueAt: testBase, SourceModifiedAt: testBase.Add(-time.Hour),
	}
	legacy, err := json.Marshal(scheduleRecordBeforeValidators{
		URL: record.URL, ProfileHandle: record.ProfileHandle, Interval: record.Interval,
		NextDueAt: record.NextDueAt, SourceModifiedAt: record.SourceModifiedAt,
	})
	if err != nil {
		t.Fatalf("encode legacy record: %v", err)
	}
	current, err := recordCodec{}.Encode(record)
	if err != nil {
		t.Fatalf("encode record: %v", err)
	}
	if !bytes.Equal(current, legacy) {
		t.Fatalf("record without validators = %s, want %s", current, legacy)
	}
	decoded, err := recordCodec{}.Decode(legacy)
	if err != nil || decoded.ValidatorBinding != nil || decoded.URL != record.URL {
		t.Fatalf("legacy record decoded as %+v, %v", decoded, err)
	}
}

func TestScheduleRecordWithValidatorsStaysReadableByEarlierReleases(t *testing.T) {
	binding := usableBinding()
	record := scheduleRecord{
		URL: validatedPageURL, ProfileHandle: "handle-a", Interval: time.Hour,
		NextDueAt: testBase, ValidatorBinding: &binding,
	}
	raw, err := recordCodec{}.Encode(record)
	if err != nil {
		t.Fatalf("encode record: %v", err)
	}
	var earlier scheduleRecordBeforeValidators
	if err := json.Unmarshal(raw, &earlier); err != nil {
		t.Fatalf("earlier release decode: %v", err)
	}
	if earlier.URL != record.URL || earlier.Interval != record.Interval ||
		!earlier.NextDueAt.Equal(record.NextDueAt) {
		t.Fatalf("earlier release read %+v, want %+v", earlier, record)
	}
}

func TestBindHTTPValidatorsStoresOnlyAUsableBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ValidatorBinding)
		stored bool
	}{
		{name: "usable", change: func(*ValidatorBinding) {}, stored: true},
		{name: "no document", change: func(b *ValidatorBinding) { b.DocumentURL = "" }},
		{name: "no observation time", change: func(b *ValidatorBinding) {
			b.ObservedAt = time.Time{}
		}},
		{name: "no validators", change: func(b *ValidatorBinding) {
			b.Validators = yagocrawlcontract.HTTPValidators{}
		}},
		{name: "malformed entity tag", change: func(b *ValidatorBinding) {
			b.Validators.ETag = "unquoted"
		}},
		{name: "no extraction generation", change: func(b *ValidatorBinding) {
			b.Revision.ExtractionGeneration = 0
		}},
		{name: "no content hash", change: func(b *ValidatorBinding) {
			b.Revision.ContentHash = ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontier, engine := boundFrontier(t)
			binding := usableBinding()
			binding.Validators.ETag = `"second"`
			tc.change(&binding)

			if err := frontier.BindHTTPValidators(
				context.Background(), validatedPageURL, binding,
			); err != nil {
				t.Fatalf("bind validators: %v", err)
			}

			stored, found := mustBinding(t, frontier)
			if found != tc.stored || (found && stored != binding) {
				t.Fatalf("stored binding = %+v, %v; want stored %v", stored, found, tc.stored)
			}
			var record scheduleRecord
			if err := json.Unmarshal(storedScheduleBytes(t, engine), &record); err != nil {
				t.Fatalf("decode stored schedule: %v", err)
			}
			if (record.ValidatorBinding != nil) != tc.stored {
				t.Fatalf(
					"persisted binding = %+v, want persisted %v",
					record.ValidatorBinding,
					tc.stored,
				)
			}
		})
	}
}

func TestBindHTTPValidatorsNeverCreatesASchedule(t *testing.T) {
	frontier, engine := openCtrlFrontier(t)

	if err := frontier.BindHTTPValidators(
		context.Background(), validatedPageURL, usableBinding(),
	); err != nil {
		t.Fatalf("bind validators: %v", err)
	}

	if len(engine.buckets[recordBucket]) != 0 {
		t.Fatalf("binding created a schedule: %v", engine.buckets[recordBucket])
	}
	if _, found := mustBinding(t, frontier); found {
		t.Fatal("binding reported without a schedule")
	}
}

func TestValidatorBindingSurvivesClaimButNotANewFullFetch(t *testing.T) {
	frontier, _ := boundFrontier(t)

	if due := claim(t, frontier, testBase.Add(2*time.Hour), 1); len(due) != 1 {
		t.Fatalf("claimed = %+v, want the bound page", due)
	}
	if _, found := mustBinding(t, frontier); !found {
		t.Fatal("claiming a due page dropped its validators")
	}
	mustObserve(t, frontier, observation{
		url: validatedPageURL, handle: "handle-a", interval: time.Hour,
		at: testBase.Add(3 * time.Hour),
	})
	if _, found := mustBinding(t, frontier); found {
		t.Fatal("a new full fetch kept the previous response validators")
	}
}

func TestValidatorBindingLeavesWithANeverRecrawlProfile(t *testing.T) {
	frontier, _ := boundFrontier(t)

	mustObserve(t, frontier, observation{
		url: validatedPageURL, handle: "handle-a", at: testBase.Add(time.Hour),
	})

	if _, found := mustBinding(t, frontier); found {
		t.Fatal("interval zero kept the response validators")
	}
	if mustRecordUnchanged(t, frontier, unchangedPage(testBase.Add(2*time.Hour))) {
		t.Fatal("not-modified observation recreated a dropped schedule")
	}
}

func TestValidatorBindingPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	first, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault: %v", err)
	}
	frontier, err := Open(first)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	mustObserve(t, frontier, observation{
		url: validatedPageURL, handle: "handle-a", interval: time.Hour, at: testBase,
	})
	if err := frontier.BindHTTPValidators(
		context.Background(), validatedPageURL, usableBinding(),
	); err != nil {
		t.Fatalf("bind validators: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	restored, err := Open(reopened)
	if err != nil {
		t.Fatalf("reopen frontier: %v", err)
	}
	if binding, found := mustBinding(t, restored); !found || binding != usableBinding() {
		t.Fatalf("binding after reopen = %+v, %v", binding, found)
	}
}
