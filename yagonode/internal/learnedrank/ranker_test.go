package learnedrank

import (
	"strconv"
	"sync"
	"testing"
)

func TestRankerConstructionAndFirstRollback(t *testing.T) {
	for _, window := range []int{0, 1, MaximumCandidateWindow + 1} {
		if _, err := NewRanker(window); err == nil {
			t.Fatalf("candidate window %d was accepted", window)
		}
	}
	ranker, err := NewRanker(DefaultCandidateWindow)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	if ranker.CandidateWindow() != DefaultCandidateWindow {
		t.Fatalf("candidate window = %d", ranker.CandidateWindow())
	}
	if _, active := ranker.ActiveSnapshot(); active || ranker.Rollback() {
		t.Fatalf("empty ranker reported an active or rollback model")
	}
	if err := ranker.Activate(Snapshot{}); err == nil {
		t.Fatalf("invalid snapshot activated")
	}
	model := mustLinearModel(t, linearWeights(nil))
	if err := ranker.Activate(mustSnapshot(t, "first", model)); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !ranker.Rollback() {
		t.Fatalf("rollback to inactive state failed")
	}
	if _, active := ranker.ActiveSnapshot(); active {
		t.Fatalf("first activation rollback stayed active")
	}
}

func TestRankerActivationRollbackHistoryBound(t *testing.T) {
	ranker, err := NewRanker(DefaultCandidateWindow)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	model := mustLinearModel(t, linearWeights(nil))
	for version := 0; version < rollbackDepth+2; version++ {
		revision := "v" + strconv.Itoa(version)
		if err := ranker.Activate(mustSnapshot(t, revision, model)); err != nil {
			t.Fatalf("Activate %s: %v", revision, err)
		}
	}
	active, ok := ranker.ActiveSnapshot()
	if !ok || active.Revision() != "v9" {
		t.Fatalf("active snapshot = %#v, %v", active, ok)
	}
	active.revision = "changed"
	activeAgain, _ := ranker.ActiveSnapshot()
	if activeAgain.Revision() != "v9" {
		t.Fatalf("active snapshot was mutable")
	}
	for expected := rollbackDepth; expected >= 1; expected-- {
		if !ranker.Rollback() {
			t.Fatalf("rollback to v%d failed", expected)
		}
		active, ok = ranker.ActiveSnapshot()
		if !ok || active.Revision() != "v"+strconv.Itoa(expected) {
			t.Fatalf("rollback active = %#v, %v", active, ok)
		}
	}
	if ranker.Rollback() {
		t.Fatalf("rollback exceeded bounded history")
	}
}

func TestRankerConcurrentActivationRollbackAndReads(t *testing.T) {
	ranker, err := NewRanker(2)
	if err != nil {
		t.Fatalf("NewRanker: %v", err)
	}
	model := mustLinearModel(t, linearWeights(nil))
	snapshots := []Snapshot{
		mustSnapshot(t, "a", model),
		mustSnapshot(t, "b", model),
	}
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for iteration := 0; iteration < 100; iteration++ {
				if worker%2 == 0 {
					if err := ranker.Activate(snapshots[iteration%len(snapshots)]); err != nil {
						t.Errorf("Activate: %v", err)
					}
				} else if !ranker.Rollback() {
					ranker.ActiveSnapshot()
				}
				ranker.ActiveSnapshot()
			}
		}(worker)
	}
	wait.Wait()
}
