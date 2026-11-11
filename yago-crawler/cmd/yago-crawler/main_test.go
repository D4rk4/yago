package main

import (
	"context"
	"errors"
	"testing"
)

func TestCrawlerWasStoppedBySignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !crawlerWasStoppedBySignal(ctx, context.Canceled) {
		t.Fatal("signal-cancelled run was not recognized as a clean stop")
	}
	if crawlerWasStoppedBySignal(context.Background(), context.Canceled) {
		t.Fatal("unexpected cancellation without signal context was treated as a clean stop")
	}
	if crawlerWasStoppedBySignal(ctx, errors.New("crawler failed")) {
		t.Fatal("crawler failure was treated as a clean signal stop")
	}
}
