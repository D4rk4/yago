package peernews

import (
	"context"
	"fmt"
	"time"
)

func (p *Pool) storeNewsRecord(
	ctx context.Context,
	record Record,
	now time.Time,
	destinations []Queue,
) (bool, error) {
	if err := p.recoverNewsRotation(ctx); err != nil {
		return false, fmt.Errorf("recover pending news publication: %w", err)
	}
	if err := p.recoverNewsAdmission(ctx); err != nil {
		return false, fmt.Errorf("recover pending news record: %w", err)
	}
	p.applyRetentionLimits()
	current, expired, err := p.inspectKnownNews(ctx, record.ID(), now)
	if err != nil {
		return false, fmt.Errorf("inspect known news: %w", err)
	}
	if current {
		return false, nil
	}
	intent := newsAdmission{record: record, destinations: destinations}
	if err := p.storeNewsAdmission(ctx, intent); err != nil {
		return false, fmt.Errorf("record pending news admission: %w", err)
	}
	stored, err := p.applyNewsAdmission(ctx, record, expired, destinations)
	if err != nil {
		p.retentionNeedsReconciliation = true

		return false, err
	}
	if err := p.clearNewsAdmission(ctx); err != nil {
		p.retentionNeedsReconciliation = true

		return false, fmt.Errorf("finish news admission: %w", err)
	}

	return stored, nil
}

func (p *Pool) applyNewsAdmission(
	ctx context.Context,
	record Record,
	expired bool,
	destinations []Queue,
) (bool, error) {
	admission, retained := p.prepareNewsRecordAdmission(record, expired, destinations)
	if !retained {
		return false, nil
	}
	applied, err := p.persistNewsRecord(ctx, record, expired, &admission)
	if err != nil {
		admission.rollback()

		return false, fmt.Errorf("store news record: %w", err)
	}
	if !applied {
		admission.rollback()

		return false, nil
	}
	admission.commit(record.ID())
	p.syncStoredState()

	return true, nil
}
