package clickcapture

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

const maximumRetainedFailedImpressionPersistences = 4096

var errImpressionPersistenceUnavailable = errors.New("impression persistence is unavailable")

type impressionIdentity [sha256.Size]byte

type impressionPersistence struct {
	done    chan struct{}
	err     error
	expires time.Time
}

func identifyImpression(token string) impressionIdentity {
	return sha256.Sum256([]byte(token))
}

func (l *impressionPreparationLifecycle) registerPersistence(
	token string,
	expires time.Time,
) *impressionPersistence {
	persistence := &impressionPersistence{done: make(chan struct{}), expires: expires}
	l.state.Lock()
	l.pruneExpiredPersistencesLocked(l.clock().UTC())
	l.persistences[identifyImpression(token)] = persistence
	l.state.Unlock()

	return persistence
}

func (l *impressionPreparationLifecycle) finishPersistence(
	token string,
	persistence *impressionPersistence,
	err error,
	emitted bool,
) {
	l.state.Lock()
	persistence.err = err
	identity := identifyImpression(token)
	if err == nil || !emitted {
		delete(l.persistences, identity)
	} else {
		l.failedPersistences++
	}
	close(persistence.done)
	l.state.Unlock()
}

func (l *impressionPreparationLifecycle) awaitPersistence(
	ctx context.Context,
	token string,
) error {
	l.state.Lock()
	l.pruneExpiredPersistencesLocked(l.clock().UTC())
	persistence := l.persistences[identifyImpression(token)]
	l.state.Unlock()
	if persistence == nil {
		return impressionPersistenceResult(ctx, nil)
	}
	select {
	case <-persistence.done:
		return impressionPersistenceResult(ctx, persistence)
	case <-ctx.Done():
		return impressionPersistenceResult(ctx, persistence)
	}
}

func impressionPersistenceResult(
	ctx context.Context,
	persistence *impressionPersistence,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wait for impression persistence: %w", err)
	}
	if persistence != nil && persistence.err != nil {
		return fmt.Errorf("wait for impression persistence: %w", persistence.err)
	}

	return nil
}

func (l *impressionPreparationLifecycle) pruneExpiredPersistencesLocked(now time.Time) {
	for identity, persistence := range l.persistences {
		if persistence.err != nil && !now.Before(persistence.expires) {
			delete(l.persistences, identity)
			l.failedPersistences--
		}
	}
}
