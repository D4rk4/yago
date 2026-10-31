package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/rwibackfill"
)

type nodeRWIBackfillAdmin struct {
	backfill nodeRWIBackfillService
}

type nodeRWIBackfillService interface {
	Start(context.Context, int64, bool) (rwibackfill.Status, error)
	Pause(context.Context) (rwibackfill.Status, error)
	Resume(context.Context, int64, bool) (rwibackfill.Status, error)
	Status(context.Context) (rwibackfill.Status, error)
}

func newNodeRWIBackfillAdmin(
	backfill nodeRWIBackfillService,
) adminui.RWIBackfillController {
	if backfill == nil {
		return nil
	}
	return nodeRWIBackfillAdmin{backfill: backfill}
}

func (c nodeRWIBackfillAdmin) Start(
	ctx context.Context,
	maximumDocuments int64,
	dryRun bool,
) (adminui.RWIBackfillStatus, error) {
	status, err := c.backfill.Start(ctx, maximumDocuments, dryRun)
	if err != nil {
		return adminui.RWIBackfillStatus{}, fmt.Errorf("start local rwi backfill: %w", err)
	}
	return adminRWIBackfillStatus(status), nil
}

func (c nodeRWIBackfillAdmin) Pause(ctx context.Context) (adminui.RWIBackfillStatus, error) {
	status, err := c.backfill.Pause(ctx)
	if err != nil {
		return adminui.RWIBackfillStatus{}, fmt.Errorf("pause local rwi backfill: %w", err)
	}
	return adminRWIBackfillStatus(status), nil
}

func (c nodeRWIBackfillAdmin) Resume(
	ctx context.Context,
	maximumDocuments int64,
	dryRun bool,
) (adminui.RWIBackfillStatus, error) {
	status, err := c.backfill.Resume(ctx, maximumDocuments, dryRun)
	if err != nil {
		return adminui.RWIBackfillStatus{}, fmt.Errorf("resume local rwi backfill: %w", err)
	}
	return adminRWIBackfillStatus(status), nil
}

func (c nodeRWIBackfillAdmin) Status(ctx context.Context) (adminui.RWIBackfillStatus, error) {
	status, err := c.backfill.Status(ctx)
	if err != nil {
		return adminui.RWIBackfillStatus{}, fmt.Errorf("read local rwi backfill status: %w", err)
	}
	return adminRWIBackfillStatus(status), nil
}

func adminRWIBackfillStatus(status rwibackfill.Status) adminui.RWIBackfillStatus {
	return adminui.RWIBackfillStatus{
		State: status.State, ScannedDocuments: status.ScannedDocuments,
		IndexedDocuments: status.IndexedDocuments, SkippedDocuments: status.SkippedDocuments,
		PostingRows: status.PostingRows, EstimatedBytes: status.EstimatedBytes,
		MaximumDocumentsRun: status.MaximumDocumentsRun, DryRun: status.DryRun,
		LastError: status.LastError, UpdatedAt: status.UpdatedAt,
	}
}
