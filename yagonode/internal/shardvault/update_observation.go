package shardvault

import (
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type vaultUpdateObserverHolder struct {
	observer vault.VaultUpdateObserver
}

type updateObservation struct {
	observer *vaultUpdateObserverHolder
	attempt  vault.UpdateAttempt
}

func (e *engine) SetVaultUpdateObserver(observer vault.VaultUpdateObserver) {
	if observer == nil {
		e.updateObserver.Store(nil)

		return
	}
	e.updateObserver.Store(&vaultUpdateObserverHolder{observer: observer})
}

func (o updateObservation) withAttempt(attempt vault.UpdateAttempt) updateObservation {
	o.attempt = attempt

	return o
}

func (o updateObservation) measure(phase vault.UpdatePhase) func() {
	if o.observer == nil || o.observer.observer == nil {
		return func() {}
	}
	started := time.Now()

	return func() {
		o.observer.observer.ObserveVaultUpdate(phase, o.attempt, time.Since(started))
	}
}
