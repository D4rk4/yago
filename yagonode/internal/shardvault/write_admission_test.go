package shardvault

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

func TestContendedUpdateLeavesViewsAvailable(t *testing.T) {
	vaulted, _ := openTestVault(t)
	values, err := vault.Register[string](vaulted, "admission", stringCodec{})
	if err != nil {
		t.Fatal(err)
	}
	key := vault.Key("drunklab")
	seedAdmissionValue(t, vaulted, values, key)
	release, first := holdAdmissionValue(vaulted, values, key)
	attempts, contended, second := contendAdmissionValue(vaulted, values, key)
	<-contended
	time.Sleep(10 * time.Millisecond)
	viewAdmissionValue(t, vaulted, values, key)
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d", attempts.Load())
	}
}

func seedAdmissionValue(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
	key vault.Key,
) {
	t.Helper()
	if err := vaulted.Update(t.Context(), func(txn *vault.Txn) error {
		if err := values.Put(txn, key, "seed"); err != nil {
			return fmt.Errorf("seed value: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func holdAdmissionValue(
	vaulted *vault.Vault,
	values *vault.Collection[string],
	key vault.Key,
) (chan struct{}, chan error) {
	holding := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- vaulted.Update(context.Background(), func(txn *vault.Txn) error {
			if err := values.Put(txn, key, "first"); err != nil {
				return fmt.Errorf("hold value: %w", err)
			}
			close(holding)
			<-release

			return nil
		})
	}()
	<-holding

	return release, done
}

func contendAdmissionValue(
	vaulted *vault.Vault,
	values *vault.Collection[string],
	key vault.Key,
) (*atomic.Int32, chan struct{}, chan error) {
	attempts := &atomic.Int32{}
	contended := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- vaulted.Update(context.Background(), func(txn *vault.Txn) error {
			attempt := attempts.Add(1)
			err := values.Put(txn, key, "second")
			if attempt == 1 {
				close(contended)
			}
			if err != nil {
				return fmt.Errorf("store second value: %w", err)
			}

			return nil
		})
	}()

	return attempts, contended, done
}

func viewAdmissionValue(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
	key vault.Key,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := vaulted.View(ctx, func(txn *vault.Txn) error {
		value, found, err := values.Get(txn, key)
		if err != nil {
			return fmt.Errorf("read value: %w", err)
		}
		if !found || value != "seed" {
			return fmt.Errorf("value=%q found=%t", value, found)
		}

		return nil
	}); err != nil {
		t.Fatalf("view during contended update: %v", err)
	}
}

func TestWriteAdmissionCancellationAndRetry(t *testing.T) {
	admission := &writeAdmission{}
	if err := admission.enterContended(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	if err := admission.enterConcurrent(ctx); err == nil {
		t.Fatal("concurrent admission ignored cancellation")
	}
	admission.leaveContended()

	if err := admission.enterConcurrent(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	if err := admission.enterContended(ctx); err == nil {
		t.Fatal("contended admission ignored cancellation")
	}
	admission.leaveConcurrent()

	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	if err := admission.enterConcurrent(ctx); err == nil {
		t.Fatal("canceled admission succeeded")
	}

	if err := admission.enterContended(t.Context()); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(4 * time.Millisecond)
		admission.leaveContended()
		close(released)
	}()
	if err := admission.enterConcurrent(t.Context()); err != nil {
		t.Fatal(err)
	}
	admission.leaveConcurrent()
	<-released
}

func TestWriteAdmissionReleasesCancellationRace(t *testing.T) {
	admission := &writeAdmission{}
	concurrentContext := &cancellationRaceContext{
		Context: context.Background(), cancelAt: 2,
	}
	if err := admission.enterConcurrent(concurrentContext); err == nil {
		t.Fatal("concurrent cancellation race succeeded")
	}
	waitingAdmission := &writeAdmission{}
	ctx := &cancellationRaceContext{Context: context.Background(), cancelAt: 2}
	if err := waitingAdmission.enterContended(ctx); err == nil {
		t.Fatal("waiting cancellation race succeeded")
	}
	if err := waitingAdmission.enterContended(t.Context()); err != nil {
		t.Fatalf("waiting cancellation race retained admission: %v", err)
	}
	waitingAdmission.leaveContended()
	acquiredContext := &cancellationRaceContext{
		Context: context.Background(), cancelAt: 3,
	}
	if err := admission.enterContended(acquiredContext); err == nil {
		t.Fatal("acquired cancellation race succeeded")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := admission.enterContended(canceled); err == nil {
		t.Fatal("canceled contended admission succeeded")
	}
	if err := admission.enterContended(t.Context()); err != nil {
		t.Fatalf("cancellation race retained admission: %v", err)
	}
	admission.leaveContended()
}

func TestContendedWriteStopsNewConcurrentAdmission(t *testing.T) {
	admission := &writeAdmission{}
	if err := admission.enterConcurrent(t.Context()); err != nil {
		t.Fatal(err)
	}
	contended := make(chan error, 1)
	go func() {
		contended <- admission.enterContended(t.Context())
	}()
	waitForContendedWriter(t, admission)
	concurrent := make(chan error, 1)
	go func() {
		concurrent <- admission.enterConcurrent(t.Context())
	}()
	select {
	case err := <-concurrent:
		t.Fatalf("concurrent write passed pending contended write: %v", err)
	case <-time.After(5 * time.Millisecond):
	}
	admission.leaveConcurrent()
	if err := <-contended; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-concurrent:
		t.Fatalf("concurrent write passed active contended write: %v", err)
	case <-time.After(5 * time.Millisecond):
	}
	admission.leaveContended()
	if err := <-concurrent; err != nil {
		t.Fatal(err)
	}
	admission.leaveConcurrent()
}

func TestContendedWriteProgressesDuringContinuousConcurrentAdmission(t *testing.T) {
	admission := &writeAdmission{}
	stop := make(chan struct{})
	var workers sync.WaitGroup
	var admissions atomic.Int64
	for range 24 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := admission.enterConcurrent(t.Context()); err != nil {
					return
				}
				admissions.Add(1)
				time.Sleep(100 * time.Microsecond)
				admission.leaveConcurrent()
			}
		}()
	}
	deadline := time.Now().Add(time.Second)
	for admissions.Load() < 24 {
		if time.Now().After(deadline) {
			t.Fatalf("concurrent admissions = %d", admissions.Load())
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if err := admission.enterContended(ctx); err != nil {
		t.Fatalf("contended admission under continuous ingress: %v", err)
	}
	admission.leaveContended()
	close(stop)
	workers.Wait()
}

func waitForContendedWriter(t *testing.T, admission *writeAdmission) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		admission.lock.Lock()
		pending := admission.pendingContended
		admission.lock.Unlock()
		if pending > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("contended writer did not enter admission")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUpdateReleasesConcurrentAdmissionWhenGlobalReadCancels(t *testing.T) {
	engine := &engine{}
	engine.globalGate.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		result <- engine.Update(ctx, func(vault.EngineTxn) error { return nil })
	}()
	waitForConcurrentWriter(t, &engine.writeAdmission)
	cancel()
	err := <-result
	engine.globalGate.Unlock()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("update error = %v, want cancellation", err)
	}
	if err := engine.writeAdmission.enterContended(t.Context()); err != nil {
		t.Fatalf("update retained concurrent admission: %v", err)
	}
	engine.writeAdmission.leaveContended()
}

func waitForConcurrentWriter(t *testing.T, admission *writeAdmission) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		admission.lock.Lock()
		concurrent := admission.concurrent
		admission.lock.Unlock()
		if concurrent > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("concurrent writer did not enter admission")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUpdateStopsWhenConcurrentAdmissionCancels(t *testing.T) {
	engine := &engine{}
	if err := engine.writeAdmission.enterContended(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	if err := engine.Update(ctx, func(vault.EngineTxn) error { return nil }); err == nil {
		t.Fatal("update ignored concurrent admission cancellation")
	}
	engine.writeAdmission.leaveContended()
}

func TestContendedUpdateAdmissionHonorsCancellation(t *testing.T) {
	engine := &engine{}
	if err := engine.writeAdmission.enterConcurrent(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	err := engine.Update(ctx, func(vault.EngineTxn) error { return errShardContended })
	engine.writeAdmission.leaveConcurrent()
	if err == nil {
		t.Fatal("contended update ignored admission cancellation")
	}
}

func TestContendedUpdateStopsWhenLayoutReadCancels(t *testing.T) {
	engine := &engine{}
	locked := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := engine.Update(ctx, func(vault.EngineTxn) error {
		go func() {
			engine.globalGate.Lock()
			close(locked)
			<-release
			engine.globalGate.Unlock()
			close(finished)
		}()
		deadline := time.Now().Add(time.Second)
		for engine.globalGate.TryRLock() {
			engine.globalGate.RUnlock()
			if time.Now().After(deadline) {
				return errors.New("layout writer did not wait")
			}
			time.Sleep(time.Millisecond)
		}

		return errShardContended
	})
	<-locked
	close(release)
	<-finished
	if err == nil {
		t.Fatal("contended update ignored layout read cancellation")
	}
}
