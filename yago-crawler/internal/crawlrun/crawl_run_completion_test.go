package crawlrun_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlrun"
)

func TestRunFinishFiresWhenLastJobSettles(t *testing.T) {
	c := crawlrun.NewCompletion()
	runID := uuid.New()
	fired := false
	gotSucceeded := false
	c.Begin(runID, func(succeeded bool) {
		fired = true
		gotSucceeded = succeeded
	})
	c.Track(runID)

	if finish, _, drained := c.Settle(runID); drained || finish != nil {
		t.Fatalf("run should not drain while a job is outstanding: drained=%v", drained)
	}
	finish, succeeded, drained := c.Settle(runID)
	if !drained {
		t.Fatal("run should drain after last job settles")
	}
	if !succeeded {
		t.Error("run with no delivery failure should drain succeeded")
	}
	finish(succeeded)
	if !fired {
		t.Error("run finish callback was not returned on drain")
	}
	if !gotSucceeded {
		t.Error("finish callback received succeeded=false for a clean run")
	}
}

func TestRunDrainsFailedWhenAnyJobFailsDelivery(t *testing.T) {
	c := crawlrun.NewCompletion()
	runID := uuid.New()
	c.Begin(runID, nil)
	c.Track(runID)

	c.Fail(runID)
	if _, _, drained := c.Settle(runID); drained {
		t.Fatal("run should not drain while a job is outstanding")
	}
	_, succeeded, drained := c.Settle(runID)
	if !drained {
		t.Fatal("run should drain after last job settles")
	}
	if succeeded {
		t.Error("run with one delivery failure should drain not-succeeded")
	}
}

func TestRunWithoutFinishDrainsSilently(t *testing.T) {
	c := crawlrun.NewCompletion()
	runID := uuid.New()
	c.Begin(runID, nil)
	finish, _, drained := c.Settle(runID)
	if !drained || finish != nil {
		t.Errorf("expected silent drain, got finish!=nil=%t drained=%t", finish != nil, drained)
	}
}

func TestReleaseDrainsCrawlerWhenLastHolderLeaves(t *testing.T) {
	c := crawlrun.NewCompletion()
	c.Hold()
	c.Hold()
	if c.Release() {
		t.Error("crawler should not drain while a holder remains")
	}
	if !c.Release() {
		t.Error("crawler should drain when the last holder releases")
	}
}

func TestReleaseDrainsOnce(t *testing.T) {
	c := crawlrun.NewCompletion()
	c.Hold()
	if !c.Release() {
		t.Fatal("first drain expected")
	}
	c.Hold()
	if c.Release() {
		t.Error("crawler must report drain only once")
	}
}
