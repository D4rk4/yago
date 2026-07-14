package websearch

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestDDGSProvidersShareProcessEngineFetchAdmission(t *testing.T) {
	first := NewDDGSProvider(DDGSConfig{})
	second := NewDDGSProvider(DDGSConfig{})

	if first.admission != processEngineFetchAdmission {
		t.Fatal("first provider does not use process engine-fetch admission")
	}
	if second.admission != first.admission {
		t.Fatal("providers do not share process engine-fetch admission")
	}
	if cap(first.admission.slots) != engineFetchConcurrency {
		t.Fatalf(
			"engine-fetch capacity = %d, want %d",
			cap(first.admission.slots),
			engineFetchConcurrency,
		)
	}
}

func TestEngineRaceCancelsWithoutLaunchingAtParserSaturation(t *testing.T) {
	admission := newEngineFetchAdmission(1)
	parseStarted := make(chan struct{}, 1)
	parseRelease := make(chan struct{})
	var parseCalls atomic.Int32
	backend := tracingEngine("bounded", Result{})
	backend.parse = func([]byte) ([]Result, error) {
		parseCalls.Add(1)
		parseStarted <- struct{}{}
		<-parseRelease

		return []Result{{Title: "answer", URL: "https://answer.example"}}, nil
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return htmlResponse(http.StatusOK, "answer"), nil
	})}
	first := NewDDGSProvider(DDGSConfig{Client: client, Now: fixedClock()})
	second := NewDDGSProvider(DDGSConfig{Client: client, Now: fixedClock()})
	first.engines = []engine{backend}
	second.engines = []engine{backend}
	first.admission = admission
	second.admission = admission
	firstDone := make(chan error, 1)
	go func() {
		_, err := first.Search(t.Context(), "first", 10)
		firstDone <- err
	}()
	<-parseStarted

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	race := newEngineRace(second, ctx, newProviderQuery("second"))
	_, _, err := race.run()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("saturated race error = %v, want cancellation", err)
	}
	if race.launched != 0 {
		t.Fatalf("saturated race launched %d engines, want 0", race.launched)
	}
	if got := parseCalls.Load(); got != 1 {
		t.Fatalf("concurrent parser calls = %d, want 1", got)
	}

	close(parseRelease)
	if err := <-firstDone; err != nil {
		t.Fatalf("first search: %v", err)
	}
	if len(admission.slots) != 0 {
		t.Fatalf("occupied engine-fetch slots after completion = %d", len(admission.slots))
	}
	if _, err := second.Search(t.Context(), "third", 10); err != nil {
		t.Fatalf("search after release: %v", err)
	}
	if got := parseCalls.Load(); got != 2 {
		t.Fatalf("parser calls after release = %d, want 2", got)
	}
}
