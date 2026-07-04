package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

func controlSourceWithRun(t *testing.T, runID, workerID string) *crawlControlSource {
	t.Helper()
	runtime := liveCrawlRuntime(t)
	if runID != "" {
		runtime.runRegistry().Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
			RunID:    runID,
			WorkerID: workerID,
			State:    yagocrawlcontract.CrawlRunRunning,
		})
	}

	return newCrawlControlSource(runtime.runRegistry(), runtime.controlRegistry())
}

func TestCrawlControlSourcePauseAndResume(t *testing.T) {
	source := controlSourceWithRun(t, "ab", "worker-1")

	for _, action := range []string{"pause", "resume"} {
		if err := source.Control(context.Background(), adminui.CrawlControlRequest{
			RunID:  "ab",
			Action: action,
		}); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
}

func TestCrawlControlSourceRejectsUnknownAction(t *testing.T) {
	source := controlSourceWithRun(t, "ab", "worker-1")

	err := source.Control(context.Background(), adminui.CrawlControlRequest{
		RunID:  "ab",
		Action: "explode",
	})
	if !errors.Is(err, errUnknownCrawlAction) {
		t.Fatalf("err = %v, want errUnknownCrawlAction", err)
	}
}

func TestCrawlControlSourceRejectsUnknownRun(t *testing.T) {
	cases := map[string]*crawlControlSource{
		"missing run":          controlSourceWithRun(t, "ab", "worker-1"),
		"blank run id":         controlSourceWithRun(t, "ab", "worker-1"),
		"run without a worker": controlSourceWithRun(t, "cd", ""),
	}
	requests := map[string]string{
		"missing run":          "zz",
		"blank run id":         "",
		"run without a worker": "cd",
	}
	for name, source := range cases {
		err := source.Control(context.Background(), adminui.CrawlControlRequest{
			RunID:  requests[name],
			Action: "pause",
		})
		if !errors.Is(err, errUnknownCrawlRun) {
			t.Fatalf("%s: err = %v, want errUnknownCrawlRun", name, err)
		}
	}
}

func TestCrawlControlKindMapping(t *testing.T) {
	cases := map[string]yagocrawlcontract.CrawlControlKind{
		"pause":  yagocrawlcontract.CrawlControlPause,
		"resume": yagocrawlcontract.CrawlControlResume,
		"cancel": yagocrawlcontract.CrawlControlCancel,
	}
	for action, want := range cases {
		got, ok := crawlControlKind(action)
		if !ok || got != want {
			t.Fatalf("crawlControlKind(%q) = %q,%v", action, got, ok)
		}
	}
	if _, ok := crawlControlKind("nope"); ok {
		t.Fatal("unknown action should not map to a kind")
	}
}

func TestCrawlControlRegistryLiveAndBare(t *testing.T) {
	if crawlControlRegistry(bareCrawlProcess{}) != nil {
		t.Fatal("bare crawl process should expose no control registry")
	}
	if crawlControlRegistry(liveCrawlRuntime(t)) == nil {
		t.Fatal("live crawl runtime should expose a control registry")
	}
}
