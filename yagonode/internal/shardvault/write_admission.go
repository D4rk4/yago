package shardvault

import (
	"context"
	"fmt"
	"sync"
)

type writeAdmission struct {
	lock             sync.Mutex
	changed          chan struct{}
	concurrent       int
	contended        bool
	pendingContended int
}

func (a *writeAdmission) enterConcurrent(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("acquire concurrent write: %w", err)
		}
		a.lock.Lock()
		if !a.contended && a.pendingContended == 0 {
			a.concurrent++
			a.lock.Unlock()
			if err := ctx.Err(); err != nil {
				a.leaveConcurrent()

				return fmt.Errorf("acquire concurrent write: %w", err)
			}

			return nil
		}
		changed := a.changedLocked()
		a.lock.Unlock()
		select {
		case <-ctx.Done():
			return fmt.Errorf("acquire concurrent write: %w", ctx.Err())
		case <-changed:
		}
	}
}

func (a *writeAdmission) leaveConcurrent() {
	a.lock.Lock()
	a.concurrent--
	if a.concurrent == 0 && a.pendingContended > 0 {
		a.notifyLocked()
	}
	a.lock.Unlock()
}

func (a *writeAdmission) enterContended(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("acquire contended write: %w", err)
	}
	a.lock.Lock()
	a.pendingContended++
	a.lock.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			a.cancelContendedWait()

			return fmt.Errorf("acquire contended write: %w", err)
		}
		a.lock.Lock()
		if !a.contended && a.concurrent == 0 {
			a.pendingContended--
			a.contended = true
			a.lock.Unlock()
			if err := ctx.Err(); err != nil {
				a.leaveContended()

				return fmt.Errorf("acquire contended write: %w", err)
			}

			return nil
		}
		changed := a.changedLocked()
		a.lock.Unlock()
		select {
		case <-ctx.Done():
			a.cancelContendedWait()

			return fmt.Errorf("acquire contended write: %w", ctx.Err())
		case <-changed:
		}
	}
}

func (a *writeAdmission) leaveContended() {
	a.lock.Lock()
	a.contended = false
	a.notifyLocked()
	a.lock.Unlock()
}

func (a *writeAdmission) cancelContendedWait() {
	a.lock.Lock()
	a.pendingContended--
	if a.pendingContended == 0 && !a.contended {
		a.notifyLocked()
	}
	a.lock.Unlock()
}

func (a *writeAdmission) changedLocked() chan struct{} {
	if a.changed == nil {
		a.changed = make(chan struct{})
	}

	return a.changed
}

func (a *writeAdmission) notifyLocked() {
	if a.changed == nil {
		return
	}
	close(a.changed)
	a.changed = nil
}
