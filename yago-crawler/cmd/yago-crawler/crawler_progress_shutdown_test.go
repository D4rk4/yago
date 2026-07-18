package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type crawlerProgressCloseFunc func(context.Context) error

func (closeProgress crawlerProgressCloseFunc) Close(ctx context.Context) error {
	return closeProgress(ctx)
}

func TestCloseCrawlerProgressPassesBoundedDetachedContext(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	called := false
	closeCrawlerProgress(parent, crawlerProgressCloseFunc(func(ctx context.Context) error {
		called = true
		if ctx.Err() != nil {
			t.Fatalf("progress context inherited cancellation: %v", ctx.Err())
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatalf("progress deadline = %v, present %t", deadline, ok)
		}

		return nil
	}), time.Second)
	if !called {
		t.Fatal("progress closer was not called")
	}
}

func TestCloseCrawlerProgressHandlesDrainFailure(t *testing.T) {
	t.Helper()
	closeCrawlerProgress(context.Background(), crawlerProgressCloseFunc(
		func(context.Context) error { return errors.New("drain failed") },
	), time.Second)
}
