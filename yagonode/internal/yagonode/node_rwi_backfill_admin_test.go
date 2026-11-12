package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/rwibackfill"
)

type nodeRWIBackfillServiceFixture struct {
	action string
	status rwibackfill.Status
	err    error
}

func (f *nodeRWIBackfillServiceFixture) Start(
	_ context.Context,
	maximum int64,
	dryRun bool,
) (rwibackfill.Status, error) {
	f.action = "start"
	f.status.MaximumDocumentsRun = maximum
	f.status.DryRun = dryRun
	return f.status, f.err
}

func (f *nodeRWIBackfillServiceFixture) Pause(context.Context) (rwibackfill.Status, error) {
	f.action = "pause"
	return f.status, f.err
}

func (f *nodeRWIBackfillServiceFixture) Resume(
	_ context.Context,
	maximum int64,
	dryRun bool,
) (rwibackfill.Status, error) {
	f.action = "resume"
	f.status.MaximumDocumentsRun = maximum
	f.status.DryRun = dryRun
	return f.status, f.err
}

func (f *nodeRWIBackfillServiceFixture) Status(context.Context) (rwibackfill.Status, error) {
	f.action = "status"
	return f.status, f.err
}

func TestNodeRWIBackfillAdminMapsStatusesAndErrors(t *testing.T) {
	if controller := newNodeRWIBackfillAdmin(nil); controller != nil {
		t.Fatal("nil backfill produced an admin controller")
	}
	updatedAt := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	service := &nodeRWIBackfillServiceFixture{status: rwibackfill.Status{
		State: "paused", ScannedDocuments: 2, IndexedDocuments: 1,
		SkippedDocuments: 1, PostingRows: 4, EstimatedBytes: 128,
		LastError: "interrupted", UpdatedAt: updatedAt,
	}}
	controller := newNodeRWIBackfillAdmin(service)
	operations := []struct {
		name string
		call func() (adminui.RWIBackfillStatus, error)
	}{
		{
			name: "start",
			call: func() (adminui.RWIBackfillStatus, error) {
				return controller.Start(t.Context(), 17, true)
			},
		},
		{
			name: "pause",
			call: func() (adminui.RWIBackfillStatus, error) {
				return controller.Pause(t.Context())
			},
		},
		{
			name: "resume",
			call: func() (adminui.RWIBackfillStatus, error) {
				return controller.Resume(t.Context(), 23, false)
			},
		},
		{
			name: "status",
			call: func() (adminui.RWIBackfillStatus, error) {
				return controller.Status(t.Context())
			},
		},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			status, err := operation.call()
			if err != nil {
				t.Fatalf("admin adapter: %v", err)
			}
			if service.action != operation.name || status.State != "paused" ||
				status.ScannedDocuments != 2 || status.IndexedDocuments != 1 ||
				status.SkippedDocuments != 1 || status.PostingRows != 4 ||
				status.EstimatedBytes != 128 || status.LastError != "interrupted" ||
				!status.UpdatedAt.Equal(updatedAt) {
				t.Fatalf(
					"operation=%s service=%s status=%+v",
					operation.name,
					service.action,
					status,
				)
			}
		})
	}
	for _, operation := range operations {
		t.Run(operation.name+" error", func(t *testing.T) {
			service.err = errors.New("fixture backfill failure")
			_, err := operation.call()
			if err == nil {
				t.Fatal("service error was not returned")
			}
			service.err = nil
		})
	}
}
