package peernews

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type pendingQueuedNews struct {
	queue       Queue
	provisional vault.Key
	record      Record
}

type newsRecordAdmission struct {
	knownProvisional vault.Key
	knownPlan        newsRetentionPlan
	queuePlan        newsRetentionPlan
	pending          []pendingQueuedNews
	actualKeys       map[string]vault.Key
}

func (p *Pool) prepareNewsRecordAdmission(
	record Record,
	expired bool,
	destinations []Queue,
) (newsRecordAdmission, bool) {
	knownExcluded := []vault.Key(nil)
	if expired {
		knownExcluded = []vault.Key{vault.Key(record.ID())}
	}
	knownProvisional := pendingKnownNewsKey(record.ID())
	knownPlan := prepareNewsRetention(
		p.knownNewsRetention,
		knownExcluded,
		[]retainedNewsRecord{{
			key: knownProvisional, tie: vault.Key(record.ID()), created: record.Created,
		}},
	)
	if !knownPlan.retains(knownProvisional) {
		knownPlan.rollback()

		return newsRecordAdmission{}, false
	}
	pending, queueRecords := pendingQueueAdmissions(record, destinations)
	queuePlan := prepareNewsRetention(p.queuedNewsRetention, nil, queueRecords)
	if !queuePlanRetainsAny(queuePlan, pending) {
		queuePlan.rollback()
		knownPlan.rollback()

		return newsRecordAdmission{}, false
	}

	return newsRecordAdmission{
		knownProvisional: knownProvisional,
		knownPlan:        knownPlan,
		queuePlan:        queuePlan,
		pending:          pending,
		actualKeys:       make(map[string]vault.Key, len(pending)),
	}, true
}

func pendingQueueAdmissions(
	record Record,
	destinations []Queue,
) ([]pendingQueuedNews, []retainedNewsRecord) {
	pending := make([]pendingQueuedNews, 0, len(destinations))
	retained := make([]retainedNewsRecord, 0, len(destinations))
	for _, destination := range destinations {
		provisional := pendingQueuedNewsKey(destination, record.ID())
		pending = append(pending, pendingQueuedNews{
			queue: destination, provisional: provisional, record: record,
		})
		retained = append(retained, queuedRetentionRecord(destination, provisional, record))
	}

	return pending, retained
}

func queuePlanRetainsAny(plan newsRetentionPlan, pending []pendingQueuedNews) bool {
	for _, item := range pending {
		if plan.retains(item.provisional) {
			return true
		}
	}

	return false
}

func (p *Pool) persistNewsRecord(
	ctx context.Context,
	record Record,
	expired bool,
	admission *newsRecordAdmission,
) (bool, error) {
	applied := false
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		clear(admission.actualKeys)
		applied = false
		if !expired && p.known.Contains(tx, vault.Key(record.ID())) {
			return nil
		}
		if err := p.writeNewsRecord(tx, record, admission); err != nil {
			return err
		}
		applied = true

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("persist news record: %w", err)
	}

	return applied, nil
}

func (p *Pool) writeNewsRecord(
	tx *vault.Txn,
	record Record,
	admission *newsRecordAdmission,
) error {
	if err := p.deleteKnownRetentionPlan(tx, admission.knownPlan); err != nil {
		return err
	}
	if err := p.known.Put(
		tx,
		vault.Key(record.ID()),
		knownMarker,
	); err != nil {
		return fmt.Errorf("remember news: %w", err)
	}
	if err := p.replaceKnownNewsCategoryForRecord(tx, vault.Key(record.ID()), record); err != nil {
		return err
	}
	if err := p.deleteQueueRetentionPlan(tx, admission.queuePlan); err != nil {
		return err
	}
	for _, item := range admission.pending {
		if !admission.queuePlan.retains(item.provisional) {
			continue
		}
		key, err := p.push(tx, item.queue, item.record)
		if err != nil {
			return fmt.Errorf("queue news: %w", err)
		}
		admission.actualKeys[string(item.provisional)] = key
	}

	return nil
}

func (a newsRecordAdmission) rollback() {
	a.queuePlan.rollback()
	a.knownPlan.rollback()
}

func (a newsRecordAdmission) commit(id string) {
	a.knownPlan.replacePendingKey(a.knownProvisional, vault.Key(id))
	for provisional, actual := range a.actualKeys {
		a.queuePlan.replacePendingKey(vault.Key(provisional), actual)
	}
}
