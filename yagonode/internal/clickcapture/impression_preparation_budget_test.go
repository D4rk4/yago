package clickcapture

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type impressionBudgetEngine struct {
	*evidenceFaultEngine
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

type queuedImpressionBudgetEngine struct {
	*evidenceFaultEngine
	startedOnce sync.Once
	started     chan struct{}
	release     chan struct{}
}

type delayedImpressionFailureEngine struct {
	*evidenceFaultEngine
	updates atomic.Int32
	started chan struct{}
	release chan struct{}
	failure error
}

func (e *queuedImpressionBudgetEngine) Update(
	ctx context.Context,
	operation func(vault.EngineTxn) error,
) error {
	e.startedOnce.Do(func() { close(e.started) })
	<-e.release

	return e.evidenceFaultEngine.Update(ctx, operation)
}

func (e *impressionBudgetEngine) Update(
	_ context.Context,
	_ func(vault.EngineTxn) error,
) error {
	close(e.started)
	<-e.release
	close(e.finished)

	return errors.New("released impression commit")
}

func (e *delayedImpressionFailureEngine) Update(
	ctx context.Context,
	operation func(vault.EngineTxn) error,
) error {
	if e.updates.Add(1) == 2 {
		close(e.started)
		<-e.release

		return e.failure
	}

	return e.evidenceFaultEngine.Update(ctx, operation)
}

func recordPreparedClick(
	ctx context.Context,
	store *Store,
	prepared PreparedImpression,
) <-chan error {
	clicked := make(chan error, 1)
	go func() {
		clicked <- store.RecordClick(
			ctx,
			prepared.Token,
			prepared.Candidates[0].URLIdentity,
			prepared.Candidates[0].Position,
		)
	}()

	return clicked
}

func waitPreparedClick(t *testing.T, clicked <-chan error) error {
	t.Helper()
	select {
	case err := <-clicked:
		return err
	case <-time.After(time.Second):
		t.Fatal("prepared click did not return")

		return nil
	}
}

func stopImpressionPreparations(store *Store) <-chan struct{} {
	stopped := make(chan struct{})
	go func() {
		store.StopImpressionPreparations()
		close(stopped)
	}()

	return stopped
}

func TestImpressionPreparationHasHardStorageWaitBound(t *testing.T) {
	previous := impressionPreparationBudget
	impressionPreparationBudget = 10 * time.Millisecond
	t.Cleanup(func() { impressionPreparationBudget = previous })
	assertBoundedImpressionPreparation(t, false)
	assertBoundedImpressionPreparation(t, true)
}

func TestRetainedImpressionPersistsAfterResponseDeadline(t *testing.T) {
	previous := impressionPreparationBudget
	impressionPreparationBudget = 5 * time.Millisecond
	t.Cleanup(func() { impressionPreparationBudget = previous })
	engine := &queuedImpressionBudgetEngine{
		evidenceFaultEngine: newEvidenceFaultEngine(),
		started:             make(chan struct{}),
		release:             make(chan struct{}),
	}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenWithSources(v, &sequenceEntropy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.PrepareImpression(t.Context(), "drunklab", []Candidate{{
		URLIdentity:     "https://about.me/drunklab",
		ClusterIdentity: "drunklab",
		Position:        1,
	}})
	if err != nil || prepared.Token == "" {
		t.Fatalf("prepared impression = %#v, error = %v", prepared, err)
	}
	select {
	case <-engine.started:
	default:
		t.Fatal("persistence did not queue before the response deadline")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.RecordClick(
		canceled,
		prepared.Token,
		prepared.Candidates[0].URLIdentity,
		prepared.Candidates[0].Position,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled immediate click error = %v", err)
	}
	clicked := recordPreparedClick(t.Context(), store, prepared)
	select {
	case err := <-clicked:
		t.Fatalf("immediate click returned before impression persistence: %v", err)
	case <-time.After(5 * time.Millisecond):
	}
	close(engine.release)
	if err := waitPreparedClick(t, clicked); err != nil {
		t.Fatal(err)
	}
	store.StopImpressionPreparations()
	aggregates, err := store.Aggregates(t.Context())
	if err != nil || len(aggregates) != 1 || aggregates[0].Query != "drunklab" {
		t.Fatalf("retained aggregates = %#v, error = %v", aggregates, err)
	}
	result := aggregates[0].Models[adjacentPairModelAssignment].Results["drunklab"]
	if result.Clicks != 1 {
		t.Fatalf("immediate click evidence = %#v", result)
	}
}

func TestFailedRetainedImpressionCannotClickOlderAggregate(t *testing.T) {
	previous := impressionPreparationBudget
	impressionPreparationBudget = 10 * time.Millisecond
	t.Cleanup(func() { impressionPreparationBudget = previous })
	sentinel := errors.New("delayed impression persistence failed")
	engine := &delayedImpressionFailureEngine{
		evidenceFaultEngine: newEvidenceFaultEngine(),
		started:             make(chan struct{}),
		release:             make(chan struct{}),
		failure:             sentinel,
	}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenWithSources(v, &sequenceEntropy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{
		URLIdentity:     "https://about.me/drunklab",
		ClusterIdentity: "drunklab",
		Position:        1,
	}
	if _, err := store.PrepareImpression(
		t.Context(),
		"drunklab",
		[]Candidate{candidate},
	); err != nil {
		t.Fatalf("seed impression: %v", err)
	}
	prepared, err := store.PrepareImpression(
		t.Context(),
		"drunklab",
		[]Candidate{candidate},
	)
	if err != nil || prepared.Token == "" {
		t.Fatalf("failed retained impression = %#v, error = %v", prepared, err)
	}
	select {
	case <-engine.started:
	default:
		t.Fatal("failed persistence did not start before response")
	}
	close(engine.release)
	store.StopImpressionPreparations()
	if err := store.RecordClick(
		t.Context(),
		prepared.Token,
		prepared.Candidates[0].URLIdentity,
		prepared.Candidates[0].Position,
	); !errors.Is(err, sentinel) {
		t.Fatalf("failed impression click error = %v", err)
	}
	aggregates, err := store.Aggregates(t.Context())
	if err != nil || len(aggregates) != 1 {
		t.Fatalf("aggregates = %#v, error = %v", aggregates, err)
	}
	result := aggregates[0].Models[adjacentPairModelAssignment].Results["drunklab"]
	if result.Impressions != 1 || result.Clicks != 0 || engine.updates.Load() != 2 {
		t.Fatalf("older aggregate changed = %#v, updates = %d", result, engine.updates.Load())
	}
}

func assertBoundedImpressionPreparation(t *testing.T, teamDraft bool) {
	t.Helper()
	engine := &impressionBudgetEngine{
		evidenceFaultEngine: newEvidenceFaultEngine(),
		started:             make(chan struct{}),
		release:             make(chan struct{}),
		finished:            make(chan struct{}),
	}
	v, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenWithSources(v, &sequenceEntropy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{
		URLIdentity:     "https://about.me/drunklab",
		ClusterIdentity: "drunklab",
		Position:        1,
	}
	started := time.Now()
	var prepared PreparedImpression
	var preparationError error
	if teamDraft {
		prepared, preparationError = store.PrepareTeamDraft(
			t.Context(),
			"drunklab",
			draftRanking("model", []Candidate{candidate}),
			draftRanking(LexicalRevision, []Candidate{candidate}),
			1,
		)
	} else {
		prepared, preparationError = store.PrepareImpression(
			t.Context(),
			"drunklab",
			[]Candidate{candidate},
		)
	}
	if preparationError != nil || prepared.Token == "" || len(prepared.Candidates) != 1 {
		t.Fatalf("prepared impression = %#v, error = %v", prepared, preparationError)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("prepare impression elapsed = %v", elapsed)
	}
	select {
	case <-engine.started:
	default:
		t.Fatal("storage update did not start")
	}
	clicked := recordPreparedClick(t.Context(), store, prepared)
	stopped := stopImpressionPreparations(store)
	select {
	case <-stopped:
		t.Fatal("impression preparations stopped before persistence returned")
	case <-time.After(5 * time.Millisecond):
	}
	close(engine.release)
	select {
	case <-engine.finished:
	case <-time.After(time.Second):
		t.Fatal("storage update did not finish")
	}
	if err := waitPreparedClick(t, clicked); err == nil {
		t.Fatal("click over failed retained persistence succeeded")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("impression preparation stop did not join persistence")
	}
	if _, err := store.PrepareImpression(
		t.Context(),
		"drunklab",
		[]Candidate{candidate},
	); !errors.Is(err, errImpressionPreparationStopped) {
		t.Fatalf("preparation after stop error = %v", err)
	}
	store.StopImpressionPreparations()
}

func TestImpressionPreparationAdmissionAndCancellation(t *testing.T) {
	var absent *Store
	absent.StopImpressionPreparations()
	preparations := newImpressionPreparationLifecycle(time.Now)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := preparations.awaitPersistence(canceled, "missing"); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("canceled missing persistence error = %v", err)
	}
	if _, err := preparations.prepareWithinBudget(canceled, func() (impressionPreparation, error) {
		return impressionPreparation{}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled preparation error = %v", err)
	}
	for range retainedImpressionPreparations {
		preparations.admission <- struct{}{}
	}
	if _, err := preparations.prepareWithinBudget(
		t.Context(),
		func() (impressionPreparation, error) {
			return impressionPreparation{}, nil
		},
	); !errors.Is(
		err,
		errImpressionPreparationBusy,
	) {
		t.Fatalf("saturated preparation error = %v", err)
	}
	for range retainedImpressionPreparations {
		<-preparations.admission
	}
	parent, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := preparations.prepareWithinBudget(parent, func() (impressionPreparation, error) {
		return impressionPreparation{}, fmt.Errorf("unexpected work")
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled admission error = %v", err)
	}
	other := newImpressionPreparationLifecycle(time.Now)
	for range retainedImpressionPreparations {
		preparations.admission <- struct{}{}
	}
	prepared, err := other.prepareWithinBudget(t.Context(), func() (impressionPreparation, error) {
		return impressionPreparation{
			prepared: PreparedImpression{Token: "isolated"},
			persist:  func(context.Context) error { return nil },
		}, nil
	})
	if err != nil || prepared.Token != "isolated" {
		t.Fatalf("isolated preparation = %#v, %v", prepared, err)
	}
	for range retainedImpressionPreparations {
		<-preparations.admission
	}
	preparations.stop()
	other.stop()
}

func TestFailedImpressionRetentionBoundsAdmissionUntilExpiry(t *testing.T) {
	clock := newMutableClock(time.Unix(1_800_000_000, 0))
	preparations := newImpressionPreparationLifecycle(clock.Now)
	expires := clock.Now().Add(time.Minute)
	sentinel := errors.New("impression persistence failed")
	for index := range maximumRetainedFailedImpressionPersistences {
		token := fmt.Sprintf("failed-%d", index)
		persistence := preparations.registerPersistence(token, expires)
		preparations.finishPersistence(token, persistence, sentinel, true)
	}
	if len(preparations.persistences) != maximumRetainedFailedImpressionPersistences ||
		preparations.failedPersistences != maximumRetainedFailedImpressionPersistences {
		t.Fatalf(
			"retained persistences = %d, failures = %d",
			len(preparations.persistences),
			preparations.failedPersistences,
		)
	}
	if _, err := preparations.prepareWithinBudget(
		t.Context(),
		func() (impressionPreparation, error) {
			return impressionPreparation{}, fmt.Errorf("unexpected preparation")
		},
	); !errors.Is(err, errImpressionPersistenceUnavailable) {
		t.Fatalf("full persistence admission error = %v", err)
	}
	if err := preparations.awaitPersistence(t.Context(), "failed-0"); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("retained persistence error = %v", err)
	}
	clock.Set(expires)
	if err := preparations.awaitPersistence(t.Context(), "failed-0"); err != nil {
		t.Fatalf("expired persistence error = %v", err)
	}
	prepared, err := preparations.prepareWithinBudget(
		t.Context(),
		func() (impressionPreparation, error) {
			return impressionPreparation{
				prepared: PreparedImpression{Token: "admitted"},
				persist:  func(context.Context) error { return nil },
				expires:  expires.Add(time.Minute),
			}, nil
		},
	)
	if err != nil || prepared.Token != "admitted" {
		t.Fatalf("post-expiry preparation = %#v, error = %v", prepared, err)
	}
	preparations.stop()
}

func TestReadyImpressionOutcomeWinsDeadline(t *testing.T) {
	sentinel := errors.New("persistence failed")
	completed := make(chan impressionPreparationOutcome, 1)
	completed <- impressionPreparationOutcome{
		prepared: PreparedImpression{Token: "prepared"},
		err:      sentinel,
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	prepared, err := resolveImpressionDeadline(
		ctx,
		completed,
		make(chan struct{}),
		PreparedImpression{Token: "deadline"},
	)
	if prepared.Token != "prepared" || !errors.Is(err, sentinel) {
		t.Fatalf("ready outcome = %#v, %v", prepared, err)
	}
}

func TestImpressionPlanningHasHardWaitBound(t *testing.T) {
	previous := impressionPreparationBudget
	impressionPreparationBudget = 5 * time.Millisecond
	t.Cleanup(func() { impressionPreparationBudget = previous })
	started := make(chan struct{})
	release := make(chan struct{})
	preparations := newImpressionPreparationLifecycle(time.Now)
	prepared, err := preparations.prepareWithinBudget(
		t.Context(),
		func() (impressionPreparation, error) {
			close(started)
			<-release

			return impressionPreparation{
				prepared: PreparedImpression{Token: "late"},
				persist:  func(context.Context) error { return nil },
			}, nil
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) || prepared.Token != "" {
		t.Fatalf("late plan = %#v, error = %v", prepared, err)
	}
	select {
	case <-started:
	default:
		t.Fatal("impression planning did not start")
	}
	close(release)
	preparations.stop()
}
