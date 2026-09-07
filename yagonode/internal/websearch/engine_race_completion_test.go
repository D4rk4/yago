package websearch

import (
	"context"
	"errors"
	"testing"
)

func TestEngineDeadlinePreservesQueuedValidatedAnswer(t *testing.T) {
	for range 32 {
		provider := NewDDGSProvider(DDGSConfig{})
		provider.engines = []engine{tracingEngine("first", Result{})}
		provider.admission = &engineFetchAdmission{slots: make(chan struct{}, 1)}
		provider.admission.slots <- struct{}{}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		race := newEngineRace(provider, ctx, newProviderQuery("needle"))
		race.attempts <- engineAttempt{backend: provider.engines[0], results: []Result{{URL: "https://example.org/needle", Title: "Needle"}}}
		results, _, err := race.run()
		if err != nil || len(results) != 1 {
			t.Fatalf("results=%+v error=%v", results, err)
		}
	}
}

func TestEngineDeadlineDoesNotInventAnAnswer(t *testing.T) {
	provider := NewDDGSProvider(DDGSConfig{})
	provider.admission = &engineFetchAdmission{slots: make(chan struct{}, 1)}
	provider.admission.slots <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results, _, err := newEngineRace(provider, ctx, newProviderQuery("needle")).run()
	if !errors.Is(err, context.Canceled) || len(results) != 0 {
		t.Fatalf("results=%+v error=%v", results, err)
	}
}
