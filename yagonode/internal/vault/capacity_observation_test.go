package vault

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type capacityObservationEngine struct {
	*scriptedEngine
	mutex   sync.Mutex
	started chan struct{}
	release chan struct{}
	calls   int
}

type capacityWaitContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (c *capacityWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })

	return c.Context.Done()
}

func (e *capacityObservationEngine) UsedBytes(ctx context.Context) (int64, error) {
	e.mutex.Lock()
	e.calls++
	call := e.calls
	e.mutex.Unlock()
	if call == 1 && e.started != nil {
		close(e.started)
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("capacity test measurement: %w", ctx.Err())
		case <-e.release:
		}
	}

	return e.scriptedEngine.UsedBytes(ctx)
}

func (e *capacityObservationEngine) callTotal() int {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return e.calls
}

func (e *capacityObservationEngine) SetQuotaBytes(quota int64) {
	e.quota = quota
}

func TestAtCapacitySharesConcurrentObservation(t *testing.T) {
	engine := &capacityObservationEngine{
		scriptedEngine: &scriptedEngine{quota: 10, used: 11},
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	v, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	const callers = 32
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			atCapacity, err := v.AtCapacity(t.Context())
			if err == nil && !atCapacity {
				err = errors.New("capacity not observed")
			}
			results <- err
		}()
	}
	close(start)
	<-engine.started
	close(engine.release)
	for range callers {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls := engine.callTotal(); calls != 1 {
		t.Fatalf("capacity measurements = %d, want 1", calls)
	}
}

func TestAtCapacityObservationExpires(t *testing.T) {
	engine := &capacityObservationEngine{
		scriptedEngine: &scriptedEngine{quota: 10, used: 1},
	}
	v, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	v.capacityUse.now = func() time.Time { return now }
	atCapacity, err := v.AtCapacity(t.Context())
	if err != nil || atCapacity {
		t.Fatalf("initial capacity = %t, %v", atCapacity, err)
	}
	engine.used = 11
	atCapacity, err = v.AtCapacity(t.Context())
	if err != nil || atCapacity {
		t.Fatalf("cached capacity = %t, %v", atCapacity, err)
	}
	now = now.Add(capacityObservationLifetime)
	atCapacity, err = v.AtCapacity(t.Context())
	if err != nil || !atCapacity {
		t.Fatalf("refreshed capacity = %t, %v", atCapacity, err)
	}
	if calls := engine.callTotal(); calls != 2 {
		t.Fatalf("capacity measurements = %d, want 2", calls)
	}
}

func TestSuccessfulUpdatesRetainCapacityObservationWithinLifetime(t *testing.T) {
	engine := &capacityObservationEngine{
		scriptedEngine: &scriptedEngine{quota: 10, used: 1},
	}
	v, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	v.capacityUse.now = func() time.Time { return now }
	if atCapacity, err := v.AtCapacity(t.Context()); err != nil || atCapacity {
		t.Fatalf("initial capacity = %t, %v", atCapacity, err)
	}
	for range 32 {
		if err := v.Update(t.Context(), func(*Txn) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if atCapacity, err := v.AtCapacity(t.Context()); err != nil || atCapacity {
			t.Fatalf("cached capacity = %t, %v", atCapacity, err)
		}
	}
	if calls := engine.callTotal(); calls != 1 {
		t.Fatalf("capacity measurements = %d, want 1", calls)
	}
	now = now.Add(capacityObservationLifetime)
	if _, err := v.AtCapacity(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls := engine.callTotal(); calls != 2 {
		t.Fatalf("capacity measurements after expiry = %d, want 2", calls)
	}
}

func TestCapacityObservationRetriesAfterMutation(t *testing.T) {
	observation := newCapacityObservation()
	started := make(chan struct{})
	release := make(chan struct{})
	type result struct {
		used int64
		err  error
	}
	resultChannel := make(chan result, 2)
	calls := 0
	read := func(ctx context.Context) (int64, error) {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("read capacity observation: %w", err)
		}
		calls++
		if calls == 1 {
			close(started)
			<-release

			return 1, nil
		}

		return 11, nil
	}
	go func() {
		used, err := observation.measure(t.Context(), read)
		resultChannel <- result{used: used, err: err}
	}()
	<-started
	waiting := make(chan struct{})
	go func() {
		used, err := observation.measure(
			&capacityWaitContext{Context: t.Context(), waiting: waiting},
			read,
		)
		resultChannel <- result{used: used, err: err}
	}()
	<-waiting
	observation.recordMutation()
	close(release)
	for range 2 {
		got := <-resultChannel
		if got.err != nil || got.used != 11 {
			t.Fatalf("capacity observation = %d, %v", got.used, got.err)
		}
	}
	if calls != 2 {
		t.Fatalf("capacity measurements = %d, want 2", calls)
	}
}

func TestAtCapacityWaitHonorsCancellation(t *testing.T) {
	engine := &capacityObservationEngine{
		scriptedEngine: &scriptedEngine{quota: 10, used: 1},
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	v, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	leader := make(chan error, 1)
	go func() {
		_, err := v.AtCapacity(t.Context())
		leader <- err
	}()
	<-engine.started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := v.AtCapacity(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v, want context cancellation", err)
	}
	close(engine.release)
	if err := <-leader; err != nil {
		t.Fatal(err)
	}
}

func TestAtCapacityMeasurementFailureIsNotCached(t *testing.T) {
	sentinel := errors.New("measurement failed")
	engine := &capacityObservationEngine{
		scriptedEngine: &scriptedEngine{quota: 10, usedErr: sentinel},
	}
	v, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.AtCapacity(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("first capacity error = %v", err)
	}
	engine.usedErr = nil
	if _, err := v.AtCapacity(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls := engine.callTotal(); calls != 2 {
		t.Fatalf("capacity measurements = %d, want 2", calls)
	}
}

func TestAtCapacitySharesConcurrentMeasurementFailure(t *testing.T) {
	sentinel := errors.New("measurement failed")
	engine := &capacityObservationEngine{
		scriptedEngine: &scriptedEngine{quota: 10, usedErr: sentinel},
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	v, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	leader := make(chan error, 1)
	go func() {
		_, err := v.AtCapacity(t.Context())
		leader <- err
	}()
	<-engine.started
	const followers = 16
	results := make(chan error, followers)
	waits := make([]chan struct{}, followers)
	for index := range followers {
		waits[index] = make(chan struct{})
		ctx := &capacityWaitContext{Context: t.Context(), waiting: waits[index]}
		go func() {
			_, err := v.AtCapacity(ctx)
			results <- err
		}()
	}
	for _, waiting := range waits {
		<-waiting
	}
	close(engine.release)
	if err := <-leader; !errors.Is(err, sentinel) {
		t.Fatalf("leader error = %v", err)
	}
	for range followers {
		if err := <-results; !errors.Is(err, sentinel) {
			t.Fatalf("follower error = %v", err)
		}
	}
	if calls := engine.callTotal(); calls != 1 {
		t.Fatalf("capacity measurements = %d, want 1", calls)
	}
	engine.usedErr = nil
	if _, err := v.AtCapacity(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls := engine.callTotal(); calls != 2 {
		t.Fatalf("capacity measurements after retry = %d, want 2", calls)
	}
}

func TestCapacityFollowersRetryCanceledLeader(t *testing.T) {
	observation := newCapacityObservation()
	leaderContext, cancelLeader := context.WithCancel(t.Context())
	leaderStarted := make(chan struct{})
	leader := make(chan error, 1)
	go func() {
		_, err := observation.measure(
			leaderContext,
			func(ctx context.Context) (int64, error) {
				close(leaderStarted)
				<-ctx.Done()

				return 0, fmt.Errorf("cancel capacity leader: %w", ctx.Err())
			},
		)
		leader <- err
	}()
	<-leaderStarted
	follower := make(chan error, 1)
	waiting := make(chan struct{})
	followerContext := &capacityWaitContext{
		Context: t.Context(),
		waiting: waiting,
	}
	go func() {
		used, err := observation.measure(
			followerContext,
			func(context.Context) (int64, error) { return 7, nil },
		)
		if err == nil && used != 7 {
			err = errors.New("follower did not retry")
		}
		follower <- err
	}()
	<-waiting
	cancelLeader()
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v", err)
	}
	if err := <-follower; err != nil {
		t.Fatal(err)
	}
}

func TestCapacityFollowerSharesSuccessfulMeasurement(t *testing.T) {
	observation := newCapacityObservation()
	started := make(chan struct{})
	release := make(chan struct{})
	leader := make(chan error, 1)
	go func() {
		_, err := observation.measure(
			t.Context(),
			func(context.Context) (int64, error) {
				close(started)
				<-release

				return 7, nil
			},
		)
		leader <- err
	}()
	<-started
	waiting := make(chan struct{})
	followerContext := &capacityWaitContext{
		Context: t.Context(),
		waiting: waiting,
	}
	follower := make(chan error, 1)
	go func() {
		used, err := observation.measure(
			followerContext,
			func(context.Context) (int64, error) {
				return 0, errors.New("follower performed duplicate measurement")
			},
		)
		if err == nil && used != 7 {
			err = fmt.Errorf("follower usage = %d", used)
		}
		follower <- err
	}()
	<-waiting
	close(release)
	if err := <-leader; err != nil {
		t.Fatal(err)
	}
	if err := <-follower; err != nil {
		t.Fatal(err)
	}
}

func TestExactUsageRefreshesCapacityObservation(t *testing.T) {
	engine := &capacityObservationEngine{
		scriptedEngine: &scriptedEngine{quota: 10, used: 1},
	}
	v, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if atCapacity, err := v.AtCapacity(t.Context()); err != nil || atCapacity {
		t.Fatalf("initial capacity = %t, %v", atCapacity, err)
	}
	engine.used = 11
	if used, err := v.UsedBytes(t.Context()); err != nil || used != 11 {
		t.Fatalf("exact usage = %d, %v", used, err)
	}
	if atCapacity, err := v.AtCapacity(t.Context()); err != nil || !atCapacity {
		t.Fatalf("refreshed capacity = %t, %v", atCapacity, err)
	}
	if calls := engine.callTotal(); calls != 2 {
		t.Fatalf("capacity measurements = %d, want 2", calls)
	}
}

func TestCapacityObservationUsesCurrentQuota(t *testing.T) {
	engine := &capacityObservationEngine{
		scriptedEngine: &scriptedEngine{quota: 20, used: 11},
	}
	v, err := New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if atCapacity, err := v.AtCapacity(t.Context()); err != nil || atCapacity {
		t.Fatalf("initial capacity = %t, %v", atCapacity, err)
	}
	v.SetQuota(10)
	if atCapacity, err := v.AtCapacity(t.Context()); err != nil || !atCapacity {
		t.Fatalf("lowered capacity = %t, %v", atCapacity, err)
	}
	v.SetQuota(20)
	if atCapacity, err := v.AtCapacity(t.Context()); err != nil || atCapacity {
		t.Fatalf("raised capacity = %t, %v", atCapacity, err)
	}
	v.SetQuota(0)
	if atCapacity, err := v.AtCapacity(t.Context()); err != nil || atCapacity {
		t.Fatalf("disabled capacity = %t, %v", atCapacity, err)
	}
	if calls := engine.callTotal(); calls != 1 {
		t.Fatalf("capacity measurements = %d, want 1", calls)
	}
}

func TestExactObservationSupersedesOlderCapacityMeasurement(t *testing.T) {
	observation := newCapacityObservation()
	started := make(chan struct{})
	release := make(chan struct{})
	type result struct {
		used int64
		err  error
	}
	leader := make(chan result, 1)
	go func() {
		used, err := observation.measure(
			t.Context(),
			func(context.Context) (int64, error) {
				close(started)
				<-release

				return 1, nil
			},
		)
		leader <- result{used: used, err: err}
	}()
	<-started
	ticket := observation.beginExactMeasurement()
	observation.recordExactMeasurement(ticket, 11)
	close(release)
	leaderResult := <-leader
	if leaderResult.err != nil || leaderResult.used != 11 {
		t.Fatalf("leader observation = %d, %v", leaderResult.used, leaderResult.err)
	}
	called := false
	used, err := observation.measure(
		t.Context(),
		func(context.Context) (int64, error) {
			called = true

			return 0, nil
		},
	)
	if err != nil || used != 11 || called {
		t.Fatalf("superseding observation = %d, %v, measured %t", used, err, called)
	}
}

func TestExactObservationSupersedesOlderCapacityErrorForAllWaiters(t *testing.T) {
	observation := newCapacityObservation()
	started := make(chan struct{})
	release := make(chan struct{})
	sentinel := errors.New("obsolete measurement")
	type result struct {
		used int64
		err  error
	}
	leader := make(chan result, 1)
	go func() {
		used, err := observation.measure(
			t.Context(),
			func(context.Context) (int64, error) {
				close(started)
				<-release

				return 0, sentinel
			},
		)
		leader <- result{used: used, err: err}
	}()
	<-started
	waiting := make(chan struct{})
	follower := make(chan result, 1)
	go func() {
		used, err := observation.measure(
			&capacityWaitContext{Context: t.Context(), waiting: waiting},
			func(context.Context) (int64, error) {
				return 0, errors.New("follower performed duplicate measurement")
			},
		)
		follower <- result{used: used, err: err}
	}()
	<-waiting
	ticket := observation.beginExactMeasurement()
	observation.recordExactMeasurement(ticket, 19)
	close(release)
	for name, resultChannel := range map[string]<-chan result{
		"leader":   leader,
		"follower": follower,
	} {
		got := <-resultChannel
		if got.err != nil || got.used != 19 {
			t.Fatalf("%s observation = %d, %v", name, got.used, got.err)
		}
	}
}

func TestLaterStartedExactObservationWinsOutOfOrderCompletion(t *testing.T) {
	observation := newCapacityObservation()
	earlier := observation.beginExactMeasurement()
	later := observation.beginExactMeasurement()
	observation.recordExactMeasurement(later, 23)
	observation.recordExactMeasurement(earlier, 7)
	called := false
	used, err := observation.measure(
		t.Context(),
		func(context.Context) (int64, error) {
			called = true

			return 0, nil
		},
	)
	if err != nil || used != 23 || called {
		t.Fatalf("exact observation = %d, %v, measured %t", used, err, called)
	}
}

func TestMutationRejectsOlderExactObservation(t *testing.T) {
	observation := newCapacityObservation()
	measurement := observation.beginExactMeasurement()
	observation.recordMutation()
	observation.recordExactMeasurement(measurement, 7)
	called := false
	used, err := observation.measure(
		t.Context(),
		func(context.Context) (int64, error) {
			called = true

			return 19, nil
		},
	)
	if err != nil || used != 19 || !called {
		t.Fatalf("capacity observation = %d, %v, measured %t", used, err, called)
	}
}
