package shardvault

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const contendedGroupLimit = 32

var errContendedGroupRolledBack = errors.New("contended write group rolled back")

type contendedWrite struct {
	update      func(vault.EngineTxn) error
	waiting     context.Context
	stopWaiting context.CancelFunc
	outcome     chan error
}

func (w *contendedWrite) settle(err error) {
	w.outcome <- err
	w.stopWaiting()
}

type contendedWriteQueue struct {
	lock   sync.Mutex
	writes []*contendedWrite
}

func (q *contendedWriteQueue) join(
	ctx context.Context,
	update func(vault.EngineTxn) error,
) *contendedWrite {
	waiting, stopWaiting := context.WithCancel(ctx)
	write := &contendedWrite{
		update:      update,
		waiting:     waiting,
		stopWaiting: stopWaiting,
		outcome:     make(chan error, 1),
	}
	q.lock.Lock()
	q.writes = append(q.writes, write)
	q.lock.Unlock()

	return write
}

func (q *contendedWriteQueue) withdraw(write *contendedWrite) bool {
	q.lock.Lock()
	defer q.lock.Unlock()
	index := slices.Index(q.writes, write)
	if index < 0 {
		return false
	}
	q.writes = slices.Delete(q.writes, index, index+1)

	return true
}

func (q *contendedWriteQueue) holds(write *contendedWrite) bool {
	q.lock.Lock()
	defer q.lock.Unlock()

	return slices.Contains(q.writes, write)
}

func (q *contendedWriteQueue) take() []*contendedWrite {
	q.lock.Lock()
	defer q.lock.Unlock()
	group := slices.Clone(q.writes[:min(len(q.writes), contendedGroupLimit)])
	q.writes = slices.Delete(q.writes, 0, len(group))

	return group
}

func (e *engine) updateContended(ctx context.Context, update func(vault.EngineTxn) error) error {
	err := e.updateInContendedGroup(ctx, update)
	if !errors.Is(err, errContendedGroupRolledBack) {
		return err
	}
	release, err := e.admitContendedWrite(ctx)
	if err != nil {
		return err
	}
	defer release()

	return e.runUpdate(update, false)
}

func (e *engine) updateInContendedGroup(
	ctx context.Context,
	update func(vault.EngineTxn) error,
) error {
	write := e.contendedWrites.join(ctx, update)
	defer write.stopWaiting()
	release, err := e.admitContendedWrite(write.waiting)
	if err != nil {
		return e.abandonContendedWrite(write, err)
	}
	defer release()
	for e.contendedWrites.holds(write) {
		e.commitContendedGroup(e.contendedWrites.take())
	}

	return <-write.outcome
}

func (e *engine) admitContendedWrite(ctx context.Context) (func(), error) {
	if err := e.writeAdmission.enterContended(ctx); err != nil {
		return nil, err
	}
	if err := acquireGlobalRead(ctx, &e.globalGate); err != nil {
		e.writeAdmission.leaveContended()

		return nil, err
	}

	return func() {
		e.globalGate.RUnlock()
		e.writeAdmission.leaveContended()
	}, nil
}

func (e *engine) abandonContendedWrite(write *contendedWrite, err error) error {
	if e.contendedWrites.withdraw(write) {
		return err
	}

	return <-write.outcome
}

func (e *engine) commitContendedGroup(group []*contendedWrite) {
	if len(group) == 1 {
		group[0].settle(e.runUpdate(group[0].update, false))

		return
	}
	outcome := e.commitContendedWrites(group)
	for _, write := range group {
		write.settle(outcome)
	}
}

func (e *engine) commitContendedWrites(group []*contendedWrite) error {
	txn := &shardTxn{engine: e, writable: true, open: make([]*bolt.Tx, len(e.shards))}
	if !txn.applyContendedWrites(group) {
		txn.rollback()

		return errContendedGroupRolledBack
	}
	if err := txn.commit(); err != nil {
		if errors.Is(err, errStructuralRootRecord) {
			return errContendedGroupRolledBack
		}

		return updateCommitError(err)
	}

	return nil
}

func (t *shardTxn) applyContendedWrites(group []*contendedWrite) (applied bool) {
	defer func() {
		if recover() != nil {
			applied = false
		}
	}()
	for _, write := range group {
		if err := write.update(t); err != nil || t.accessErr != nil {
			return false
		}
	}

	return true
}

func updateCommitError(err error) error {
	if storageAtCapacityError(err) {
		return vault.ErrAtCapacity
	}

	return fmt.Errorf("update storage: %w", err)
}
