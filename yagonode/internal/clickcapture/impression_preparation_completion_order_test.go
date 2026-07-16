package clickcapture

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestImpressionPreparationReleasesAdmissionBeforePublishingOutcome(t *testing.T) {
	sentinel := errors.New("preparation failed")
	for _, scenario := range []struct {
		name             string
		preparationError error
	}{
		{name: "persisted"},
		{name: "preparation failure", preparationError: sentinel},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			verifyImpressionPreparationCompletionOrder(t, scenario.preparationError)
		})
	}
}

func verifyImpressionPreparationCompletionOrder(t *testing.T, preparationError error) {
	t.Helper()
	preparations := newImpressionPreparationLifecycle(time.Now)
	preparations.admission = make(chan struct{}, 1)
	if err := preparations.admit(); err != nil {
		t.Fatal(err)
	}
	planned := make(chan impressionPreparation)
	completed := make(chan impressionPreparationOutcome)
	preparationFinished := make(chan struct{})
	go preparations.runImpressionPreparation(impressionPreparationTask{
		responseContext:    t.Context(),
		persistenceContext: context.WithoutCancel(t.Context()),
		prepare: func() (impressionPreparation, error) {
			close(preparationFinished)
			if preparationError != nil {
				return impressionPreparation{}, preparationError
			}

			return impressionPreparation{
				prepared: PreparedImpression{Token: "prepared"},
				persist:  func(context.Context) error { return nil },
				expires:  time.Now().Add(time.Minute),
			}, nil
		},
		planned:   planned,
		completed: completed,
		abandoned: make(chan struct{}),
	})
	<-preparationFinished
	if preparationError == nil {
		<-planned
	}
	if !awaitEmptyImpressionAdmission(preparations.admission) {
		<-completed
		t.Fatal("preparation outcome blocked admission release")
	}
	stopStarted := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		close(stopStarted)
		preparations.stop()
		close(stopped)
	}()
	<-stopStarted
	select {
	case <-stopped:
		t.Fatal("preparation lifecycle stopped before outcome delivery")
	case <-time.After(5 * time.Millisecond):
	}
	outcome := <-completed
	if !errors.Is(outcome.err, preparationError) {
		t.Fatalf("outcome error = %v", outcome.err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("preparation lifecycle did not stop after outcome delivery")
	}
}

func awaitEmptyImpressionAdmission(admission <-chan struct{}) bool {
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	probe := time.NewTicker(time.Millisecond)
	defer probe.Stop()
	for {
		if len(admission) == 0 {
			return true
		}
		select {
		case <-probe.C:
		case <-deadline.C:
			return false
		}
	}
}
