package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
)

type acceptingCrawlControl struct{}

func (acceptingCrawlControl) Enqueue(
	string,
	yagocrawlcontract.CrawlControlDirective,
) bool {
	return true
}

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

	return newCrawlControlSource(runtime.runRegistry(), acceptingCrawlControl{}, nil)
}

func TestCrawlControlSourceSteersRun(t *testing.T) {
	source := controlSourceWithRun(t, "ab", "worker-1")

	for _, action := range []string{"pause", "resume", "cancel", "set_rate"} {
		if err := source.Control(context.Background(), adminui.CrawlControlRequest{
			RunID:          "ab",
			Action:         action,
			PagesPerMinute: 30,
		}); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
}

func TestCrawlControlSourceRemembersRateForMonitorPrefill(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	runtime.runRegistry().Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "ab", WorkerID: "worker-1", State: yagocrawlcontract.CrawlRunRunning,
	})
	source := newCrawlControlSource(runtime.runRegistry(), acceptingCrawlControl{}, nil)

	if err := source.Control(context.Background(), adminui.CrawlControlRequest{
		RunID: "ab", Action: "set_rate", PagesPerMinute: 45,
	}); err != nil {
		t.Fatalf("set_rate: %v", err)
	}
	if got := runtime.runRegistry().Recent()[0].PagesPerMinute; got != 45 {
		t.Fatalf("remembered rate = %d, want 45", got)
	}

	// A non-rate action leaves the remembered rate intact.
	if err := source.Control(context.Background(), adminui.CrawlControlRequest{
		RunID: "ab", Action: "pause",
	}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if got := runtime.runRegistry().Recent()[0].PagesPerMinute; got != 45 {
		t.Fatalf("rate after pause = %d, want 45 preserved", got)
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
		"pause":    yagocrawlcontract.CrawlControlPause,
		"resume":   yagocrawlcontract.CrawlControlResume,
		"cancel":   yagocrawlcontract.CrawlControlCancel,
		"set_rate": yagocrawlcontract.CrawlControlSetRate,
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

type recordingRestarter struct {
	handles []string
	err     error
}

func (r *recordingRestarter) Restart(
	_ context.Context,
	handle string,
) (crawldispatch.Accepted, error) {
	r.handles = append(r.handles, handle)

	return crawldispatch.Accepted{ProfileHandle: handle}, r.err
}

func restartControlSource(
	t *testing.T,
	restarter crawlRestarter,
) *crawlControlSource {
	t.Helper()
	runtime := liveCrawlRuntime(t)
	runtime.runRegistry().Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID:         "ab",
		WorkerID:      "worker-1",
		ProfileHandle: "HandleAAAAAA",
		State:         yagocrawlcontract.CrawlRunFinished,
	})

	return newCrawlControlSource(runtime.runRegistry(), acceptingCrawlControl{}, restarter)
}

func TestCrawlControlSourceRejectsOfflineWorker(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	runtime.runRegistry().Record(context.Background(), yagocrawlcontract.CrawlRunProgress{
		RunID: "ab", WorkerID: "offline-worker", State: yagocrawlcontract.CrawlRunRunning,
	})
	source := newCrawlControlSource(runtime.runRegistry(), runtime.controlRegistry(), nil)
	err := source.Control(context.Background(), adminui.CrawlControlRequest{
		RunID: "ab", Action: "pause",
	})
	if !errors.Is(err, errCrawlWorkerOffline) {
		t.Fatalf("error = %v, want errCrawlWorkerOffline", err)
	}
}

func TestCrawlControlSourceRestartsFinishedRunByProfile(t *testing.T) {
	restarter := &recordingRestarter{}
	source := restartControlSource(t, restarter)

	if err := source.Control(context.Background(), adminui.CrawlControlRequest{
		RunID:  "ab",
		Action: "restart",
	}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if len(restarter.handles) != 1 || restarter.handles[0] != "HandleAAAAAA" {
		t.Fatalf("restarted handles = %v", restarter.handles)
	}
}

func TestCrawlControlSourceRestartRejections(t *testing.T) {
	failing := &recordingRestarter{err: errors.New("queue down")}
	source := restartControlSource(t, failing)
	if err := source.Control(context.Background(), adminui.CrawlControlRequest{
		RunID:  "ab",
		Action: "restart",
	}); err == nil {
		t.Fatal("expected the restarter failure to surface")
	}

	if err := source.Control(context.Background(), adminui.CrawlControlRequest{
		RunID:  "zz",
		Action: "restart",
	}); !errors.Is(err, errUnknownCrawlRun) {
		t.Fatalf("unknown run err = %v", err)
	}
	if err := source.Control(context.Background(), adminui.CrawlControlRequest{
		Action: "restart",
	}); !errors.Is(err, errUnknownCrawlRun) {
		t.Fatalf("blank run err = %v", err)
	}

	withoutRestarter := restartControlSource(t, nil)
	if err := withoutRestarter.Control(context.Background(), adminui.CrawlControlRequest{
		RunID:  "ab",
		Action: "restart",
	}); !errors.Is(err, errUnknownCrawlAction) {
		t.Fatalf("nil restarter err = %v", err)
	}
}

func TestCrawlRestartSourceKeepsNilDispatcherNil(t *testing.T) {
	if crawlRestartSource(nil) != nil {
		t.Fatal("nil dispatcher must stay a nil restarter")
	}
	if crawlRestartSource(&crawldispatch.Dispatcher{}) == nil {
		t.Fatal("real dispatcher must become a restarter")
	}
}
