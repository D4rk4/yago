package firefoxfetch

import (
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/browserpool"
)

type BrowserFailureReason = browserpool.FailureReason

const (
	BrowserFailureSlotDeadline = browserpool.FailureSlotDeadline
	BrowserFailureCooldown     = browserpool.FailureCooldown
	BrowserFailureLaunch       = browserpool.FailureLaunch
	BrowserFailureRender       = browserpool.FailureRender
)

type BrowserPoolState = browserpool.State

type BrowserPoolObserver = browserpool.Observer

type browserPoolObservation struct {
	legacyDeadline func()
	observer       browserpool.Observer
}

func (o browserPoolObservation) observeWait(elapsed time.Duration) {
	if o.observer != nil {
		o.observer.ObserveBrowserSlotWait(elapsed)
	}
}

func (o browserPoolObservation) observeState(state browserpool.State) {
	if o.observer != nil {
		o.observer.ObserveBrowserPoolState(state)
	}
}

func (o browserPoolObservation) observeFailure(reason browserpool.FailureReason) {
	if reason == BrowserFailureSlotDeadline && o.legacyDeadline != nil {
		o.legacyDeadline()
	}
	if o.observer != nil {
		o.observer.ObserveBrowserFailure(reason)
	}
}
