package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

type crawlerGrowthWaitScript struct {
	waits []bool
}

func (script *crawlerGrowthWaitScript) WaitForGrowth(context.Context) bool {
	allowed := script.waits[0]
	script.waits = script.waits[1:]

	return allowed
}

type crawlerGrowthRecheckScript struct {
	waits  []bool
	checks []error
}

type crawlerStateGrowthWaitScript struct {
	waits  []bool
	errors []error
}

func (script *crawlerStateGrowthWaitScript) WaitForGrowth(
	context.Context,
) (bool, error) {
	allowed := script.waits[0]
	err := script.errors[0]
	script.waits = script.waits[1:]
	script.errors = script.errors[1:]

	return allowed, err
}

func (script *crawlerGrowthRecheckScript) WaitForGrowth(context.Context) bool {
	allowed := script.waits[0]
	script.waits = script.waits[1:]

	return allowed
}

func (script *crawlerGrowthRecheckScript) CheckGrowth() error {
	err := script.checks[0]
	script.checks = script.checks[1:]

	return err
}

func TestCrawlerNewGrowthAdmissionStopsWithStoragePressure(t *testing.T) {
	storage := &crawlerGrowthWaitScript{waits: []bool{false}}
	if allowed, err := newCrawlerNewGrowthAdmission(
		storage,
		nil,
	).WaitForGrowth(t.Context()); allowed ||
		err != nil {
		t.Fatal("fresh growth admitted under storage pressure")
	}
}

func TestCrawlerNewGrowthAdmissionStopsWhenStateWaitIsCancelled(t *testing.T) {
	storage := &crawlerGrowthWaitScript{waits: []bool{true}}
	state := &crawlerStateGrowthWaitScript{waits: []bool{false}, errors: []error{nil}}
	if allowed, err := newCrawlerNewGrowthAdmission(
		storage,
		state,
	).WaitForGrowth(t.Context()); allowed ||
		err != nil {
		t.Fatal("fresh growth admitted after the state wait stopped")
	}
}

func TestCrawlerNewGrowthAdmissionRechecksStorageAfterStateWait(t *testing.T) {
	want := errors.New("storage changed")
	storage := &crawlerGrowthRecheckScript{
		waits:  []bool{true, true},
		checks: []error{want, nil},
	}
	state := &crawlerStateGrowthWaitScript{
		waits:  []bool{true, true},
		errors: []error{nil, nil},
	}
	if allowed, err := newCrawlerNewGrowthAdmission(
		storage,
		state,
	).WaitForGrowth(t.Context()); !allowed ||
		err != nil {
		t.Fatalf("fresh growth recovery = %t, %v", allowed, err)
	}
}

func TestCrawlerNewGrowthAdmissionAllowsOptionalHealthyState(t *testing.T) {
	storage := &crawlerGrowthWaitScript{waits: []bool{true}}
	state := &crawlerStateGrowthWaitScript{waits: []bool{true}, errors: []error{nil}}
	if allowed, err := newCrawlerNewGrowthAdmission(
		storage,
		state,
	).WaitForGrowth(t.Context()); !allowed ||
		err != nil {
		t.Fatalf("healthy fresh growth = %t, %v", allowed, err)
	}
	if allowed, err := newCrawlerNewGrowthAdmission(
		nil,
		nil,
	).WaitForGrowth(t.Context()); !allowed ||
		err != nil {
		t.Fatalf("optional growth admission = %t, %v", allowed, err)
	}
}

func TestCrawlerNewGrowthAdmissionPropagatesStateInspectionFailure(t *testing.T) {
	want := errors.New("inspect state")
	state := &crawlerStateGrowthWaitScript{waits: []bool{false}, errors: []error{want}}
	allowed, err := newCrawlerNewGrowthAdmission(nil, state).WaitForGrowth(t.Context())
	if allowed || !errors.Is(err, want) {
		t.Fatalf("state inspection result = %t, %v", allowed, err)
	}
}

func TestCrawlerNewGrowthAdmissionWaitsForLiveFrontierMaximumRaise(t *testing.T) {
	checkpoint, err := frontiercheckpoint.OpenWithStateMaximum(
		filepath.Join(t.TempDir(), "frontier.db"),
		1,
		nil,
	)
	if err != nil {
		t.Fatalf("open frontier checkpoint: %v", err)
	}
	defer func() { _ = checkpoint.Close() }()
	if err := checkpoint.CheckGrowth(); !errors.Is(err, frontiercheckpoint.ErrStateMaximum) {
		t.Fatalf("initial frontier growth error = %v", err)
	}
	type waitResult struct {
		allowed bool
		err     error
	}
	result := make(chan waitResult, 1)
	go func() {
		allowed, err := newCrawlerNewGrowthAdmission(nil, checkpoint).WaitForGrowth(t.Context())
		result <- waitResult{allowed: allowed, err: err}
	}()
	select {
	case <-result:
		t.Fatal("fresh order did not wait at the frontier maximum")
	case <-time.After(20 * time.Millisecond):
	}
	checkpoint.SetStateMaximumBytes(1 << 30)
	select {
	case result := <-result:
		if !result.allowed || result.err != nil {
			t.Fatalf("live maximum raise result = %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("fresh order remained stuck after the live maximum raise")
	}
}
