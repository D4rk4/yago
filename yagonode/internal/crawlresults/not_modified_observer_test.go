package crawlresults_test

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type notModifiedOutcomeObserver struct {
	recordingObserver
	recorded int
	refused  int
}

func (o *notModifiedOutcomeObserver) ObserveNotModifiedRecorded() { o.recorded++ }

func (o *notModifiedOutcomeObserver) ObserveNotModifiedRefused() { o.refused++ }

func TestNotModifiedObservationOutcomeIsObservedOnlyWhenDecided(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		conditional               scriptedConditionalRecrawl
		ownership                 crawlresults.OwnershipCheck
		wantRecorded, wantRefused int
		wantDeferred              int
	}{
		{
			name:         "recorded",
			conditional:  scriptedConditionalRecrawl{recorded: true},
			wantRecorded: 1,
		},
		{name: "refused", wantRefused: 1},
		{
			name:         "recording failed",
			conditional:  scriptedConditionalRecrawl{recordErr: errors.New("vault closed")},
			wantDeferred: 1,
		},
		{
			name:        "unowned profile",
			conditional: scriptedConditionalRecrawl{recorded: true},
			ownership:   fakeOwnership{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer := &notModifiedOutcomeObserver{}
			run := newConditionalConsumerRun(&tc.conditional)
			run.ownership = tc.ownership
			run.observer = observer
			settled := make(chan string, 1)

			run.absorb(t, settlingDelivery(notModifiedBatch("https://example.org/page"), settled))
			awaitSettlement(t, settled)

			if observer.recorded != tc.wantRecorded || observer.refused != tc.wantRefused ||
				observer.deferred != tc.wantDeferred {
				t.Fatalf("observed recorded=%d refused=%d deferred=%d, want %d %d %d",
					observer.recorded, observer.refused, observer.deferred,
					tc.wantRecorded, tc.wantRefused, tc.wantDeferred)
			}
		})
	}
}
