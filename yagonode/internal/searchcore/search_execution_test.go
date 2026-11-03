package searchcore

import (
	"context"
	"reflect"
	"testing"
)

func TestSearchProgressContextHelpers(t *testing.T) {
	ctx := t.Context()
	if FullShardSearchRequired(ctx) {
		t.Fatal("full shard search unexpectedly enabled")
	}
	full := WithFullShardSearch(ctx)
	if !FullShardSearchRequired(full) {
		t.Fatal("full shard search flag was not retained")
	}

	var observed SearchProgress
	observedCount := 0
	withObserver := WithSearchProgressObserver(ctx, func(progress SearchProgress) {
		observed = progress
		observedCount++
	})
	progress := SearchProgress{CompletedShards: 2, TotalShards: 4}
	if got := SearchProgressObserverFrom(withObserver); got == nil {
		t.Fatal("search progress observer was not retained")
	}
	ReportSearchProgress(withObserver, progress)
	if observedCount != 1 || !reflect.DeepEqual(observed, progress) {
		t.Fatalf("observed=%+v count=%d", observed, observedCount)
	}
	ReportSearchProgress(ctx, progress)
	if observedCount != 1 {
		t.Fatalf("progress without observer was reported %d times", observedCount)
	}
	withoutObserver := WithoutSearchProgressObserver(withObserver)
	if SearchProgressObserverFrom(withoutObserver) != nil {
		t.Fatal("progress observer remained enabled")
	}
	ReportSearchProgress(withoutObserver, progress)
	if observedCount != 1 {
		t.Fatalf("suppressed progress was reported %d times", observedCount)
	}
	if SearchProgressObserverFrom(context.Background()) != nil {
		t.Fatal("background context unexpectedly has an observer")
	}
}
