package learnedrank

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	DefaultCandidateWindow = 100
	MaximumCandidateWindow = 256
	rollbackDepth          = 8
)

type Ranker struct {
	candidateWindow int
	active          atomic.Pointer[Snapshot]
	activationLock  sync.Mutex
	history         []*Snapshot
}

func NewRanker(candidateWindow int) (*Ranker, error) {
	if candidateWindow < 2 || candidateWindow > MaximumCandidateWindow {
		return nil, fmt.Errorf(
			"candidate window must be between 2 and %d",
			MaximumCandidateWindow,
		)
	}

	return &Ranker{candidateWindow: candidateWindow}, nil
}

func (r *Ranker) CandidateWindow() int {
	return r.candidateWindow
}

func (r *Ranker) Activate(snapshot Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("activate learned ranking model: %w", err)
	}
	snapshotCopy := snapshot
	r.activationLock.Lock()
	if len(r.history) == rollbackDepth {
		copy(r.history, r.history[1:])
		r.history = r.history[:rollbackDepth-1]
	}
	r.history = append(r.history, r.active.Load())
	r.active.Store(&snapshotCopy)
	r.activationLock.Unlock()

	return nil
}

func (r *Ranker) Rollback() bool {
	r.activationLock.Lock()
	defer r.activationLock.Unlock()
	if len(r.history) == 0 {
		return false
	}
	last := len(r.history) - 1
	r.active.Store(r.history[last])
	r.history = r.history[:last]

	return true
}

func (r *Ranker) ActiveSnapshot() (Snapshot, bool) {
	snapshot := r.active.Load()
	if snapshot == nil {
		return Snapshot{}, false
	}

	return *snapshot, true
}
