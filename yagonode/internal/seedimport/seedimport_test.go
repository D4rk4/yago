package seedimport_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/seedimport"
)

func openStore(t *testing.T) *seedimport.Store {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	now := func() time.Time { return time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC) }
	store, err := seedimport.Open(v, now)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return store
}

func TestRecordAndGetSuccess(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	url := "https://seeds.example/seed.txt"

	if _, found, err := store.Get(ctx, url); err != nil || found {
		t.Fatalf("Get before record = %v, %v; want not found", found, err)
	}

	if err := store.Record(ctx, url, 12, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	status, found, err := store.Get(ctx, url)
	if err != nil || !found {
		t.Fatalf("Get after record = %v, %v", found, err)
	}
	if !status.OK || status.Seeds != 12 || status.Error != "" {
		t.Fatalf("status = %+v, want ok/12/no-error", status)
	}
	if status.LastImport.IsZero() {
		t.Fatal("last import time not recorded")
	}
}

func TestRecordFailureKeepsError(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	url := "https://seeds.example/seed.txt"

	if err := store.Record(ctx, url, 0, errors.New("boom")); err != nil {
		t.Fatalf("Record: %v", err)
	}

	status, found, err := store.Get(ctx, url)
	if err != nil || !found {
		t.Fatalf("Get = %v, %v", found, err)
	}
	if status.OK || status.Error != "boom" || status.Seeds != 0 {
		t.Fatalf("status = %+v, want failure with message", status)
	}
}

func TestRecordUpsertsLatest(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	url := "https://seeds.example/seed.txt"

	if err := store.Record(ctx, url, 0, errors.New("first failed")); err != nil {
		t.Fatalf("Record fail: %v", err)
	}
	if err := store.Record(ctx, url, 7, nil); err != nil {
		t.Fatalf("Record success: %v", err)
	}

	status, _, err := store.Get(ctx, url)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !status.OK || status.Seeds != 7 || status.Error != "" {
		t.Fatalf("status = %+v, want the latest success to overwrite", status)
	}
}
