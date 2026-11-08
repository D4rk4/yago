package yagonode

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestCorpusSignalRefreshWaitOutlivesClosedTrustSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		oldDelay := newCorpusSignalRefreshDelay
		t.Cleanup(func() { newCorpusSignalRefreshDelay = oldDelay })
		ticks := make(chan time.Time, 1)
		newCorpusSignalRefreshDelay = func(time.Duration) (<-chan time.Time, func()) {
			return ticks, func() {}
		}
		closedChanges := make(chan struct{})
		close(closedChanges)
		refresh := &corpusSignalRefresh{
			trust: &mutableHostTrustPolicy{changes: closedChanges},
		}
		waited := make(chan bool, 1)
		go func() { waited <- refresh.waitFor(t.Context(), defaultCorpusSignalRefreshInterval) }()
		synctest.Wait()
		select {
		case <-waited:
			t.Fatal("closed trust source ended the refresh delay")
		default:
		}
		ticks <- time.Now()
		if didWait := <-waited; !didWait {
			t.Fatal("closed trust source stopped refresh delay")
		}
	})
}
