package yagonode

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerreputation"
)

type peerReputationLedgerFixture struct {
	lock         sync.Mutex
	last         uint64
	sequenceErr  error
	observeErr   error
	snapshotErr  error
	attempts     int
	snapshotAt   []time.Time
	attempted    []peerreputation.ObservationBatch
	applications []peerreputation.ObservationBatch
	superseded   uint64
}

func (fixture *peerReputationLedgerFixture) LastBatchSequence(context.Context) (uint64, error) {
	return fixture.last, fixture.sequenceErr
}

func (fixture *peerReputationLedgerFixture) ObserveBatch(
	_ context.Context,
	batch peerreputation.ObservationBatch,
) (peerreputation.BatchApplication, error) {
	fixture.lock.Lock()
	defer fixture.lock.Unlock()
	fixture.attempts++
	fixture.attempted = append(fixture.attempted, batch)
	if fixture.superseded != 0 {
		sequence := fixture.superseded
		fixture.superseded = 0

		return peerreputation.BatchApplication{
			Superseded: true, LastSequence: sequence,
		}, nil
	}
	if fixture.observeErr != nil {
		return peerreputation.BatchApplication{}, fixture.observeErr
	}
	fixture.applications = append(fixture.applications, batch)

	return peerreputation.BatchApplication{
		Applied: true, LastSequence: batch.Sequence,
	}, nil
}

func (fixture *peerReputationLedgerFixture) Snapshot(
	_ context.Context,
	at time.Time,
) (peerreputation.Snapshot, error) {
	fixture.lock.Lock()
	defer fixture.lock.Unlock()
	fixture.snapshotAt = append(fixture.snapshotAt, at)

	return peerreputation.Snapshot{}, fixture.snapshotErr
}

func TestPeerReputationObserverContinuesPersistedSequence(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ledger := &peerReputationLedgerFixture{last: 7}
	observer, err := newPeerReputationObserver(ctx, ledger)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	observed := []peerreputation.Observation{
		{
			Peer: "peer", NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
			ObservedAt: time.Unix(1, 0).UTC(),
		},
		{
			Peer: "peer-two", NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
			ObservedAt: time.Unix(2, 0).UTC(),
		},
	}
	observer.Observe(context.Background(), observed)
	observer.Observe(context.Background(), observed)
	applications := waitForPeerApplications(t, ledger, 2)
	if applications[0].Sequence != 8 || applications[1].Sequence != 9 {
		t.Fatalf("applications = %#v", applications)
	}
	observed[0].Peer = "changed"
	if applications[0].Observations[0].Peer != "peer" {
		t.Fatal("observer did not preserve the submitted batch")
	}
	if _, err := observer.Snapshot(context.Background(), time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}
	ledger.lock.Lock()
	lastSnapshotAt := ledger.snapshotAt[len(ledger.snapshotAt)-1]
	ledger.lock.Unlock()
	if !lastSnapshotAt.Equal(time.Unix(2, 0).UTC()) {
		t.Fatalf("snapshot time = %v", lastSnapshotAt)
	}
}

func TestPeerReputationObserverFailurePaths(t *testing.T) {
	sequenceFailure := &peerReputationLedgerFixture{sequenceErr: errors.New("read")}
	if _, err := newPeerReputationObserver(context.Background(), sequenceFailure); err == nil {
		t.Fatal("created observer after sequence read failure")
	}
	if _, err := newPeerReputationObserver(
		context.Background(),
		&peerReputationLedgerFixture{last: math.MaxUint64},
	); err == nil {
		t.Fatal("created observer after sequence exhaustion")
	}
	if _, err := newPeerReputationObserver(
		context.Background(),
		&peerReputationLedgerFixture{snapshotErr: errors.New("snapshot")},
	); err == nil {
		t.Fatal("created observer after snapshot failure")
	}

	observations := []peerreputation.Observation{{
		Peer: "peer", NetworkGroup: "group", Outcome: peerreputation.OutcomeFailure,
		ObservedAt: time.Unix(1, 0).UTC(),
	}}
	failed := &peerReputationLedgerFixture{observeErr: errors.New("write")}
	observer, err := newPeerReputationObserver(context.Background(), failed)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	observer.Observe(context.Background(), observations)
	waitForPeerAttempts(t, failed, peerReputationMaximumAttempts)
	if observer.sequence.Load() != 1 {
		t.Fatalf("sequence advanced to %d", observer.sequence.Load())
	}
	failed.lock.Lock()
	failed.observeErr = nil
	failed.snapshotErr = errors.New("refresh")
	failed.lock.Unlock()
	waitForPeerApplications(t, failed, 1)
	waitForPeerSnapshots(t, failed, 2)
	failed.lock.Lock()
	attempted := append([]peerreputation.ObservationBatch(nil), failed.attempted...)
	failed.lock.Unlock()
	if len(attempted) <= peerReputationMaximumAttempts {
		t.Fatalf("attempted batches = %#v", attempted)
	}
	for _, batch := range attempted {
		if batch.Sequence != 1 || !slices.Equal(batch.Observations, observations) {
			t.Fatalf("retried batch = %#v", batch)
		}
	}
	observer.Close()
	observer.sequence.Store(0)
	observer.persist(context.Background(), observations)
	observer.Observe(context.Background(), nil)
	observer.Observe(context.Background(), observations)
	var absent *peerReputationObserver
	absent.Observe(context.Background(), observations)
	absent.Close()

	full := &peerReputationObserver{queue: make(chan []peerreputation.Observation, 1)}
	full.queue <- observations
	full.Observe(context.Background(), observations)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if waitPeerReputationRetry(canceled, 0) {
		t.Fatal("peer reputation retry ignored cancellation")
	}
}

type blockingPeerReputationLedger struct {
	peerReputationLedgerFixture
	started            chan struct{}
	release            chan struct{}
	startedOnce        sync.Once
	ignoreCancellation bool
}

func (ledger *blockingPeerReputationLedger) ObserveBatch(
	ctx context.Context,
	batch peerreputation.ObservationBatch,
) (peerreputation.BatchApplication, error) {
	ledger.startedOnce.Do(func() { close(ledger.started) })
	if ledger.ignoreCancellation {
		<-ledger.release

		return ledger.peerReputationLedgerFixture.ObserveBatch(ctx, batch)
	}
	select {
	case <-ledger.release:
	case <-ctx.Done():
		return peerreputation.BatchApplication{}, fmt.Errorf(
			"blocked peer reputation observation: %w",
			ctx.Err(),
		)
	}

	return ledger.peerReputationLedgerFixture.ObserveBatch(ctx, batch)
}

func TestPeerReputationObservationDoesNotBlockSearch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ledger := &blockingPeerReputationLedger{
		started: make(chan struct{}), release: make(chan struct{}),
	}
	observer, err := newPeerReputationObserver(ctx, ledger)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	returned := make(chan struct{})
	go func() {
		observer.Observe(context.Background(), []peerreputation.Observation{{
			Peer: "peer", NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
			ObservedAt: time.Unix(1, 0).UTC(),
		}})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("observation blocked on storage")
	}
	select {
	case <-ledger.started:
	case <-time.After(time.Second):
		t.Fatal("observation worker did not reach storage")
	}
	close(ledger.release)
	waitForPeerApplications(t, &ledger.peerReputationLedgerFixture, 1)
}

func TestPeerReputationCloseDrainsAcceptedObservations(t *testing.T) {
	ledger := &peerReputationLedgerFixture{}
	observer, err := newPeerReputationObserver(t.Context(), ledger)
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 3; index++ {
		observer.Observe(context.Background(), []peerreputation.Observation{{
			Peer:         peerreputation.SignedPeerIdentity(fmt.Sprintf("peer-%d", index)),
			NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
			ObservedAt: time.Unix(int64(index), 0).UTC(),
		}})
	}
	observer.Close()
	if applications := waitForPeerApplications(t, ledger, 3); len(applications) != 3 ||
		applications[0].Sequence != 1 || applications[2].Sequence != 3 {
		t.Fatalf("drained applications = %#v", applications)
	}
}

func TestPeerReputationCloseBoundsBlockedDrain(t *testing.T) {
	ledger := &blockingPeerReputationLedger{
		started: make(chan struct{}), release: make(chan struct{}), ignoreCancellation: true,
	}
	observer, err := newPeerReputationObserver(t.Context(), ledger)
	if err != nil {
		t.Fatal(err)
	}
	observer.shutdownWait = time.Millisecond
	observer.Observe(context.Background(), []peerreputation.Observation{{
		Peer: "peer", NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
		ObservedAt: time.Unix(1, 0).UTC(),
	}})
	select {
	case <-ledger.started:
	case <-time.After(time.Second):
		t.Fatal("blocked drain did not start")
	}
	started := time.Now()
	observer.Close()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("bounded close took %v", elapsed)
	}
	select {
	case <-observer.done:
		t.Fatal("uninterruptible persistence ended before release")
	default:
	}
	close(ledger.release)
	select {
	case <-observer.done:
	case <-time.After(time.Second):
		t.Fatal("released persistence did not finish")
	}
}

func TestPeerReputationObserverRebasesSupersededSequence(t *testing.T) {
	ledger := &peerReputationLedgerFixture{superseded: 7}
	observer, err := newPeerReputationObserver(t.Context(), ledger)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	observer.Observe(context.Background(), []peerreputation.Observation{{
		Peer: "peer", NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
		ObservedAt: time.Unix(1, 0).UTC(),
	}})
	applications := waitForPeerApplications(t, ledger, 1)
	if applications[0].Sequence != 8 || observer.sequence.Load() != 9 {
		t.Fatalf("rebased applications = %#v, next = %d", applications, observer.sequence.Load())
	}
}

func TestPeerReputationObserverTerminalBranches(t *testing.T) {
	observation := []peerreputation.Observation{{
		Peer: "peer", NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
		ObservedAt: time.Unix(1, 0).UTC(),
	}}
	superseded := &peerReputationLedgerFixture{superseded: math.MaxUint64}
	observer := &peerReputationObserver{ledger: superseded}
	observer.sequence.Store(1)
	observer.persist(t.Context(), observation)
	if observer.sequence.Load() != 0 {
		t.Fatalf("superseded terminal sequence = %d", observer.sequence.Load())
	}

	applied := &peerReputationLedgerFixture{}
	observer = &peerReputationObserver{ledger: applied}
	observer.sequence.Store(math.MaxUint64)
	observer.persist(t.Context(), observation)
	if observer.sequence.Load() != 0 {
		t.Fatalf("applied terminal sequence = %d", observer.sequence.Load())
	}

	failed := &peerReputationLedgerFixture{observeErr: errors.New("failed")}
	observer = &peerReputationObserver{ledger: failed}
	observer.sequence.Store(1)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	observer.persist(canceled, observation)

	done := make(chan struct{})
	observer = &peerReputationObserver{done: done}
	observer.run(canceled)
	select {
	case <-done:
	default:
		t.Fatal("canceled worker did not finish")
	}
}

func TestPeerReputationCloseSharesCompletion(t *testing.T) {
	ledger := &blockingPeerReputationLedger{
		started: make(chan struct{}), release: make(chan struct{}),
	}
	observer, err := newPeerReputationObserver(t.Context(), ledger)
	if err != nil {
		t.Fatal(err)
	}
	observer.Observe(context.Background(), []peerreputation.Observation{{
		Peer: "peer", NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
		ObservedAt: time.Unix(1, 0).UTC(),
	}})
	select {
	case <-ledger.started:
	case <-time.After(time.Second):
		t.Fatal("persistence did not start")
	}
	closed := make(chan struct{}, 2)
	for range 2 {
		go func() {
			observer.Close()
			closed <- struct{}{}
		}()
	}
	select {
	case <-closed:
		t.Fatal("concurrent close returned before shared drain")
	case <-time.After(20 * time.Millisecond):
	}
	close(ledger.release)
	for range 2 {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("concurrent close did not finish")
		}
	}
}

func TestPeerReputationObserveAndCloseSynchronizeAdmission(t *testing.T) {
	ledger := &peerReputationLedgerFixture{}
	observer, err := newPeerReputationObserver(t.Context(), ledger)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var observers sync.WaitGroup
	for index := range 64 {
		observers.Add(1)
		go func() {
			defer observers.Done()
			<-start
			observer.Observe(context.Background(), []peerreputation.Observation{{
				Peer:         peerreputation.SignedPeerIdentity(fmt.Sprintf("peer-%d", index)),
				NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
				ObservedAt: time.Unix(int64(index+1), 0).UTC(),
			}})
		}()
	}
	close(start)
	observer.Close()
	observers.Wait()
	observer.Observe(context.Background(), []peerreputation.Observation{{
		Peer: "late", NetworkGroup: "group", Outcome: peerreputation.OutcomeSuccess,
		ObservedAt: time.Unix(100, 0).UTC(),
	}})
}

func TestPeerReputationSnapshotRefreshesOutsideSearch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ledger := &peerReputationLedgerFixture{}
	refresh := make(chan time.Time, 1)
	var stopped atomic.Bool
	observer, err := newPeerReputationObserverWithRefresh(
		ctx,
		ledger,
		refresh,
		func() { stopped.Store(true) },
	)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(3, 0).UTC()
	refresh <- at
	waitForPeerSnapshots(t, ledger, 2)
	observer.Close()
	observer.Close()
	if !stopped.Load() {
		t.Fatal("peer reputation refresh ticker was not stopped")
	}
	ledger.lock.Lock()
	last := ledger.snapshotAt[len(ledger.snapshotAt)-1]
	ledger.lock.Unlock()
	if !last.Equal(at) {
		t.Fatalf("periodic snapshot time = %v, want %v", last, at)
	}
}

func waitForPeerApplications(
	t *testing.T,
	ledger *peerReputationLedgerFixture,
	want int,
) []peerreputation.ObservationBatch {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ledger.lock.Lock()
		applications := append([]peerreputation.ObservationBatch(nil), ledger.applications...)
		ledger.lock.Unlock()
		if len(applications) >= want {
			return applications
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("peer reputation applications did not reach %d", want)

	return nil
}

func waitForPeerAttempts(t *testing.T, ledger *peerReputationLedgerFixture, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ledger.lock.Lock()
		attempts := ledger.attempts
		ledger.lock.Unlock()
		if attempts >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("peer reputation attempts did not reach %d", want)
}

func waitForPeerSnapshots(t *testing.T, ledger *peerReputationLedgerFixture, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ledger.lock.Lock()
		snapshots := len(ledger.snapshotAt)
		ledger.lock.Unlock()
		if snapshots >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("peer reputation snapshots did not reach %d", want)
}

func TestPeerReputationNetworkGroups(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		host string
		want peerreputation.NetworkGroupKey
	}{
		{name: "IPv4", host: "192.0.2.129", want: "ipv4:192.0.2.0/24"},
		{name: "IPv6", host: "2001:db8:abcd:1234::1", want: "ipv6:2001:db8:abcd::/48"},
		{name: "hostname", host: "peer.example", want: "hostname:unresolved"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host, err := yagomodel.ParseHost(test.host)
			if err != nil {
				t.Fatal(err)
			}
			port, err := yagomodel.ParsePort("8090")
			if err != nil {
				t.Fatal(err)
			}
			seed := yagomodel.Seed{IP: yagomodel.Some(host), Port: yagomodel.Some(port)}
			if got := peerReputationNetworkGroup(seed); got != test.want {
				t.Fatalf("group = %q, want %q", got, test.want)
			}
		})
	}
	if got := peerReputationNetworkGroup(yagomodel.Seed{}); got != "" {
		t.Fatalf("missing address group = %q", got)
	}
}
