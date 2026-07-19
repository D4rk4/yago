package frontiercheckpoint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFrontierStateGrowthGateAppliesMaximumLive(t *testing.T) {
	path := testCheckpointPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create checkpoint directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("12345678"), 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}
	gate := newFrontierStateGrowthGate(path, 8)
	if err := gate.CheckGrowth(); !errors.Is(err, ErrStateMaximum) {
		t.Fatalf("growth error = %v, want %v", err, ErrStateMaximum)
	}
	type waitResult struct {
		allowed bool
		err     error
	}
	result := make(chan waitResult, 1)
	go func() {
		allowed, err := gate.WaitForGrowth(t.Context())
		result <- waitResult{allowed: allowed, err: err}
	}()
	gate.SetMaximumBytes(9)
	if result := <-result; !result.allowed || result.err != nil {
		t.Fatalf("live maximum update result = %+v", result)
	}
}

func TestFrontierStateGrowthGateHandlesDisabledMissingInspectionAndCancellation(t *testing.T) {
	directory := t.TempDir()
	missing := newFrontierStateGrowthGate(filepath.Join(directory, "missing.db"), 1)
	if err := missing.CheckGrowth(); err != nil {
		t.Fatalf("missing state growth = %v", err)
	}
	missing.SetMaximumBytes(0)
	if err := missing.CheckGrowth(); err != nil {
		t.Fatalf("disabled state growth = %v", err)
	}
	loop := filepath.Join(directory, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatalf("create symlink loop: %v", err)
	}
	inspection := newFrontierStateGrowthGate(loop, 1)
	if err := inspection.CheckGrowth(); err == nil || errors.Is(err, ErrStateMaximum) {
		t.Fatalf("inspection error = %v", err)
	}
	if allowed, err := inspection.WaitForGrowth(t.Context()); allowed || err == nil ||
		errors.Is(err, ErrStateMaximum) {
		t.Fatalf("inspection wait result = %t, %v", allowed, err)
	}
	path := filepath.Join(directory, "full.db")
	if err := os.WriteFile(path, []byte("full"), 0o600); err != nil {
		t.Fatalf("write full state: %v", err)
	}
	below := newFrontierStateGrowthGate(path, 5)
	if err := below.CheckGrowth(); err != nil {
		t.Fatalf("below maximum growth = %v", err)
	}
	blocked := newFrontierStateGrowthGate(path, 4)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if allowed, err := blocked.WaitForGrowth(ctx); allowed || err != nil {
		t.Fatalf("canceled state growth wait = %t, %v", allowed, err)
	}
	checkpoint, err := Open(filepath.Join(directory, "checkpoint.db"))
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}
	defer func() { _ = checkpoint.Close() }()
	checkpoint.SetStateMaximumBytes(0)
	if err := checkpoint.CheckGrowth(); err != nil {
		t.Fatalf("checkpoint growth while disabled = %v", err)
	}
	if allowed, err := checkpoint.WaitForGrowth(t.Context()); !allowed || err != nil {
		t.Fatalf("checkpoint growth wait while disabled = %t, %v", allowed, err)
	}
}
