package peernews

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type publicationRotation struct {
	record      Record
	destination Queue
	provisional vault.Key
	valid       bool
}

type publicationRotationOutcome struct {
	actual  vault.Key
	applied bool
}

func (p *Pool) nextPublicationCandidate(
	ctx context.Context,
	now time.Time,
) (vault.Key, Record, bool, bool, error) {
	var (
		key    vault.Key
		record Record
		found  bool
		valid  bool
	)
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		return p.queue.Scan(
			tx,
			queuePrefix(Outgoing),
			func(k vault.Key, wire string) (bool, error) {
				key = append(vault.Key(nil), k...)
				found = true
				candidate, parseErr := parseRecord(wire, time.Time{})
				matches := false
				if parseErr == nil {
					var err error
					matches, err = p.knownRecordMatches(tx, candidate)
					if err != nil {
						return false, err
					}
				}
				if parseErr == nil &&
					newsCreationAdmitted(candidate.Created, now, candidate.Category) && matches {
					record = candidate
					valid = true
				}

				return false, nil
			},
		)
	})
	if err != nil {
		return nil, Record{}, false, false, fmt.Errorf("read outgoing news: %w", err)
	}

	return key, record, found, valid, nil
}

func (p *Pool) rotatePublication(
	ctx context.Context,
	key vault.Key,
	record Record,
	valid bool,
) (Record, bool, error) {
	p.applyRetentionLimits()
	rotation, err := p.preparePublicationRotation(ctx, key, record, valid)
	if err != nil {
		return Record{}, false, err
	}
	plan := prepareNewsRetention(
		p.queuedNewsRetention,
		[]vault.Key{key},
		rotation.pendingRecords(),
	)
	outcome, err := p.applyPublicationRotation(ctx, key, rotation, plan)
	if err != nil {
		plan.rollback()
		p.retentionNeedsReconciliation = true

		return Record{}, false, err
	}
	if !outcome.applied {
		plan.rollback()
		if err := p.finishPublicationRotation(ctx, rotation.valid); err != nil {
			return Record{}, false, err
		}

		return Record{}, false, nil
	}
	if outcome.actual != nil {
		plan.replacePendingKey(rotation.provisional, outcome.actual)
	}
	p.syncStoredState()
	if err := p.finishPublicationRotation(ctx, rotation.valid); err != nil {
		return Record{}, false, err
	}

	return rotation.record, rotation.valid, nil
}

func (p *Pool) preparePublicationRotation(
	ctx context.Context,
	key vault.Key,
	record Record,
	valid bool,
) (publicationRotation, error) {
	rotation := publicationRotation{record: record, destination: Outgoing, valid: valid}
	if !valid {
		return rotation, nil
	}
	original := record
	rotation.record.Distributed++
	rotation.valid = newsRecordAdmitted(rotation.record)
	if !rotation.valid {
		return rotation, nil
	}
	if rotation.record.Distributed >= distributionLimit {
		rotation.destination = Published
	}
	rotation.provisional = pendingQueuedNewsKey(rotation.destination, rotation.record.ID())
	if err := p.storeNewsRotation(ctx, newsRotation{
		source: key, original: original, rotated: rotation.record,
		destination: rotation.destination,
	}); err != nil {
		return publicationRotation{}, fmt.Errorf("record news publication rotation: %w", err)
	}

	return rotation, nil
}

func (r publicationRotation) pendingRecords() []retainedNewsRecord {
	if !r.valid {
		return nil
	}

	return []retainedNewsRecord{
		queuedRetentionRecord(r.destination, r.provisional, r.record),
	}
}

func (p *Pool) applyPublicationRotation(
	ctx context.Context,
	key vault.Key,
	rotation publicationRotation,
	plan newsRetentionPlan,
) (publicationRotationOutcome, error) {
	var outcome publicationRotationOutcome
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		outcome = publicationRotationOutcome{}
		if !p.queue.Contains(tx, key) {
			return nil
		}
		if err := p.deleteQueueRetentionPlan(tx, plan); err != nil {
			return err
		}
		if rotation.valid && plan.retains(rotation.provisional) {
			actual, err := p.push(tx, rotation.destination, rotation.record)
			if err != nil {
				return err
			}
			outcome.actual = actual
		}
		outcome.applied = true

		return nil
	})
	if err != nil {
		return publicationRotationOutcome{}, fmt.Errorf("rotate news publication: %w", err)
	}

	return outcome, nil
}

func (p *Pool) finishPublicationRotation(ctx context.Context, recorded bool) error {
	if !recorded {
		return nil
	}
	if err := p.clearNewsRotation(ctx); err != nil {
		p.retentionNeedsReconciliation = true

		return fmt.Errorf("finish news publication rotation: %w", err)
	}

	return nil
}
