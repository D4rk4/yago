package crawlrun_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yacycrawler/internal/crawlrun"
)

func TestRunFinishFiresWhenLastJobSettles(t *testing.T) {
	c := crawlrun.NewCompletion()
	runID := uuid.New()
	fired := false
	c.Begin(runID, func() { fired = true })
	c.Track(runID)

	if finish, drained := c.Settle(runID); drained || finish != nil {
		t.Fatalf("run should not drain while a job is outstanding: drained=%v", drained)
	}
	finish, drained := c.Settle(runID)
	if !drained {
		t.Fatal("run should drain after last job settles")
	}
	finish()
	if !fired {
		t.Error("run finish callback was not returned on drain")
	}
}

func TestRunWithoutFinishDrainsSilently(t *testing.T) {
	c := crawlrun.NewCompletion()
	runID := uuid.New()
	c.Begin(runID, nil)
	finish, drained := c.Settle(runID)
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
