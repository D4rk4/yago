package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type growthAdmission interface {
	CheckGrowth() error
}

type admittedPostingReceiver struct {
	inner     rwi.PostingReceiver
	admission growthAdmission
}

func (r admittedPostingReceiver) Receive(
	ctx context.Context,
	entries []yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	if len(entries) > 0 && storageGrowthRejected(r.admission) {
		return rwi.Receipt{Busy: true, Pause: receiveBusyPauseMilliseconds}, nil
	}
	receipt, err := r.inner.Receive(ctx, entries)
	if err != nil {
		return receipt, fmt.Errorf("receive admitted postings: %w", err)
	}

	return receipt, nil
}

type admittedURLReceiver struct {
	inner     urlmeta.URLReceiver
	admission growthAdmission
}

func (r admittedURLReceiver) Receive(
	ctx context.Context,
	rows []yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	if len(rows) > 0 && storageGrowthRejected(r.admission) {
		return urlmeta.Receipt{Busy: true}, nil
	}
	receipt, err := r.inner.Receive(ctx, rows)
	if err != nil {
		return receipt, fmt.Errorf("receive admitted URL metadata: %w", err)
	}

	return receipt, nil
}

func storageGrowthRejected(admission growthAdmission) bool {
	return admission != nil && admission.CheckGrowth() != nil
}

func storageWithGrowthAdmission(storage nodeStorage, admission growthAdmission) nodeStorage {
	if admission == nil {
		return storage
	}
	storage.postingReceiver = admittedPostingReceiver{
		inner: storage.postingReceiver, admission: admission,
	}
	storage.urlReceiver = admittedURLReceiver{
		inner: storage.urlReceiver, admission: admission,
	}
	return storage
}
