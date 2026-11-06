package recrawlfrontier

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestRecordUnchangedPageAdvancesTheScheduleOfTheBoundRevision(t *testing.T) {
	frontier, _ := boundFrontier(t)
	observedAt := testBase.Add(90 * time.Minute)
	page := unchangedPage(observedAt)
	page.Validators = &yagocrawlcontract.HTTPValidators{ETag: `"refreshed"`}

	if !mustRecordUnchanged(t, frontier, page) {
		t.Fatal("matching not-modified observation was refused")
	}

	if due := claim(t, frontier, observedAt.Add(time.Hour-time.Second), 1); len(due) != 0 {
		t.Fatalf("claimed before the advanced due time: %+v", due)
	}
	if due := claim(t, frontier, observedAt.Add(time.Hour), 1); len(due) != 1 {
		t.Fatalf("claimed at the advanced due time = %+v, want the page", due)
	}
	binding, _ := mustBinding(t, frontier)
	if !binding.ObservedAt.Equal(observedAt) || binding.Validators != *page.Validators ||
		binding.Revision != storedRevision() {
		t.Fatalf("binding after not-modified = %+v", binding)
	}
}

func TestRecordUnchangedPageReplayKeepsTheDurableState(t *testing.T) {
	frontier, engine := boundFrontier(t)
	page := unchangedPage(testBase.Add(90 * time.Minute))
	mustRecordUnchanged(t, frontier, page)
	recorded := storedScheduleBytes(t, engine)
	dueEntries := len(engine.buckets[dueBucket])

	page.Validators = &yagocrawlcontract.HTTPValidators{ETag: `"replayed"`}
	if !mustRecordUnchanged(t, frontier, page) {
		t.Fatal("replayed not-modified observation was not confirmed")
	}

	if replayed := storedScheduleBytes(t, engine); !bytes.Equal(replayed, recorded) {
		t.Fatalf("replay changed the schedule: %s, want %s", replayed, recorded)
	}
	if len(engine.buckets[dueBucket]) != dueEntries {
		t.Fatalf("replay changed the due index: %v", engine.buckets[dueBucket])
	}
}

func TestRecordUnchangedPageKeepsValidatorsWhenTheRefreshIsUnusable(t *testing.T) {
	for _, tc := range []struct {
		name       string
		validators *yagocrawlcontract.HTTPValidators
	}{
		{name: "absent"},
		{name: "malformed", validators: &yagocrawlcontract.HTTPValidators{ETag: "unquoted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontier, _ := boundFrontier(t)
			page := unchangedPage(testBase.Add(time.Hour))
			page.Validators = tc.validators

			if !mustRecordUnchanged(t, frontier, page) {
				t.Fatal("matching not-modified observation was refused")
			}

			binding, _ := mustBinding(t, frontier)
			if binding.Validators != usableBinding().Validators {
				t.Fatalf("validators = %+v, want the bound validators", binding.Validators)
			}
		})
	}
}

func TestRecordUnchangedPageRefusesAnObservationThatDoesNotMatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*UnchangedPage)
	}{
		{name: "older than the binding", change: func(p *UnchangedPage) {
			p.ObservedAt = testBase.Add(-time.Second)
		}},
		{name: "without observation time", change: func(p *UnchangedPage) {
			p.ObservedAt = time.Time{}
		}},
		{name: "foreign profile", change: func(p *UnchangedPage) { p.ProfileHandle = "handle-b" }},
		{name: "other content", change: func(p *UnchangedPage) {
			p.Revision.ContentHash = "sha256:other"
		}},
		{name: "other extraction generation", change: func(p *UnchangedPage) {
			p.Revision.ExtractionGeneration++
		}},
		{name: "other fetch time", change: func(p *UnchangedPage) {
			p.Revision.FetchedAt = testBase.Add(time.Second)
		}},
		{name: "unscheduled page", change: func(p *UnchangedPage) {
			p.URL = "https://a.example/unscheduled"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontier, engine := boundFrontier(t)
			before := storedScheduleBytes(t, engine)
			page := unchangedPage(testBase.Add(time.Hour))
			tc.change(&page)

			if mustRecordUnchanged(t, frontier, page) {
				t.Fatal("mismatched not-modified observation was recorded")
			}

			if after := storedScheduleBytes(t, engine); !bytes.Equal(after, before) {
				t.Fatalf("refused observation changed the schedule: %s", after)
			}
			if len(engine.buckets[recordBucket]) != 1 {
				t.Fatalf("refused observation created a schedule: %v", engine.buckets[recordBucket])
			}
		})
	}
}

func TestRecordUnchangedPageRefusesAScheduleWithoutValidators(t *testing.T) {
	frontier, _ := openCtrlFrontier(t)
	mustObserve(t, frontier, observation{
		url: validatedPageURL, handle: "handle-a", interval: time.Hour, at: testBase,
	})

	if mustRecordUnchanged(t, frontier, unchangedPage(testBase.Add(time.Hour))) {
		t.Fatal("not-modified observation recorded without bound validators")
	}
}

func TestRecordUnchangedPageAppliesTheSourceModificationHint(t *testing.T) {
	frontier, _ := boundFrontier(t)
	observedAt := testBase.Add(time.Hour)
	page := unchangedPage(observedAt)
	page.SourceModifiedAt = observedAt.Add(-50 * time.Minute)

	mustRecordUnchanged(t, frontier, page)

	if due := claim(t, frontier, observedAt.Add(10*time.Minute), 1); len(due) != 1 {
		t.Fatalf("claimed at the hinted due time = %+v, want the page", due)
	}
}

func TestValidatorBindingReportsUnreadableSchedules(t *testing.T) {
	frontier, engine := openCtrlFrontier(t)
	engine.seed(recordBucket, hashKey(t, validatedPageURL), []byte("{corrupt"))
	ctx := context.Background()

	if err := frontier.BindHTTPValidators(ctx, validatedPageURL, usableBinding()); err == nil {
		t.Fatal("binding over an unreadable schedule succeeded")
	}
	if _, found, err := frontier.ValidatorBinding(ctx, validatedPageURL); err == nil || found {
		t.Fatalf("reading an unreadable schedule = %v, %v; want an error", found, err)
	}
	recorded, err := frontier.RecordUnchangedPage(ctx, unchangedPage(testBase.Add(time.Hour)))
	if err == nil || recorded {
		t.Fatalf("recording over an unreadable schedule = %v, %v; want an error", recorded, err)
	}
}

func TestValidatorBindingReportsScheduleWriteFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inject func(*ctrlEngine)
	}{
		{name: "schedule write", inject: func(e *ctrlEngine) { e.failPut[recordBucket] = true }},
		{name: "due index removal", inject: func(e *ctrlEngine) { e.failDel[dueBucket] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontier, engine := boundFrontier(t)
			tc.inject(engine)

			recorded, err := frontier.RecordUnchangedPage(
				context.Background(), unchangedPage(testBase.Add(time.Hour)),
			)

			if err == nil || recorded {
				t.Fatalf("failed write recorded = %v, %v; want an error", recorded, err)
			}
		})
	}
	frontier, engine := boundFrontier(t)
	engine.failPut[recordBucket] = true
	if err := frontier.BindHTTPValidators(
		context.Background(), validatedPageURL, usableBinding(),
	); err == nil {
		t.Fatal("binding with a failing schedule write succeeded")
	}
}

func TestStoredBindingThatFailsValidationIsNeitherIssuedNorMatched(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ValidatorBinding)
		usable bool
	}{
		{name: "valid", change: func(*ValidatorBinding) {}, usable: true},
		{name: "malformed entity tag", change: func(b *ValidatorBinding) {
			b.Validators.ETag = "unquoted"
		}},
		{name: "oversized content hash", change: func(b *ValidatorBinding) {
			b.Revision.ContentHash = strings.Repeat("h", yagocrawlcontract.MaximumDocumentMetadataBytes+1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontier, engine := openCtrlFrontier(t)
			binding := usableBinding()
			tc.change(&binding)
			raw, err := json.Marshal(scheduleRecord{
				URL: validatedPageURL, ProfileHandle: "handle-a", Interval: time.Hour,
				NextDueAt: testBase.Add(time.Hour), ValidatorBinding: &binding,
			})
			if err != nil {
				t.Fatalf("encode stored record: %v", err)
			}
			engine.seed(recordBucket, hashKey(t, validatedPageURL), raw)
			page := unchangedPage(testBase.Add(time.Hour))
			page.Revision = binding.Revision

			if _, found := mustBinding(t, frontier); found != tc.usable {
				t.Fatalf("binding issued = %v, want %v", found, tc.usable)
			}
			if recorded := mustRecordUnchanged(t, frontier, page); recorded != tc.usable {
				t.Fatalf("not-modified recorded = %v, want %v", recorded, tc.usable)
			}
		})
	}
}
