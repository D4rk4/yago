package peernews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var newsAdmissionKey = vault.Key("admission")

type newsAdmission struct {
	record       Record
	destinations []Queue
}

type storedNewsAdmission struct {
	Wire         []byte  `json:"wire"`
	Destinations []Queue `json:"destinations"`
}

func (p *Pool) storeNewsAdmission(ctx context.Context, admission newsAdmission) error {
	raw, _ := json.Marshal(storedNewsAdmission{
		Wire: []byte(admission.record.WireForm()), Destinations: admission.destinations,
	})
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := p.cleanup.Put(tx, newsAdmissionKey, string(raw)); err != nil {
			return fmt.Errorf("store news admission: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update news admission: %w", err)
	}

	return nil
}

func (p *Pool) readNewsAdmission(ctx context.Context) (newsAdmission, bool, error) {
	var admission newsAdmission
	found := false
	invalid := false
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		raw, present, err := p.storedNewsCleanup(tx, newsAdmissionKey)
		if err != nil {
			return err
		}
		if !present {
			return nil
		}
		decoded, valid := decodeNewsAdmission(raw)
		if !valid {
			return fmt.Errorf("%w: invalid news admission", vault.ErrCorruptValue)
		}
		admission = decoded
		found = true

		return nil
	})
	if err != nil {
		if !errors.Is(err, vault.ErrCorruptValue) {
			return newsAdmission{}, false, fmt.Errorf("read news admission: %w", err)
		}
		invalid = true
	}
	if invalid {
		if err := p.clearNewsAdmission(ctx); err != nil {
			return newsAdmission{}, false, fmt.Errorf("discard invalid news admission: %w", err)
		}
	}

	return admission, found, nil
}

func decodeNewsAdmission(raw string) (newsAdmission, bool) {
	var stored storedNewsAdmission
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return newsAdmission{}, false
	}
	record, err := parseRecord(string(stored.Wire), time.Time{})
	if err != nil || len(stored.Destinations) == 0 {
		return newsAdmission{}, false
	}
	seen := make(map[Queue]bool, len(stored.Destinations))
	for _, destination := range stored.Destinations {
		if seen[destination] || !validNewsQueue(destination) {
			return newsAdmission{}, false
		}
		seen[destination] = true
	}

	return newsAdmission{record: record, destinations: stored.Destinations}, true
}

func validNewsQueue(queue Queue) bool {
	for _, candidate := range newsQueues {
		if queue == candidate {
			return true
		}
	}

	return false
}

func (p *Pool) clearNewsAdmission(ctx context.Context) error {
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := p.cleanup.Delete(tx, newsAdmissionKey); err != nil {
			return fmt.Errorf("clear news admission: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update cleared news admission: %w", err)
	}

	return nil
}

func (p *Pool) recoverNewsAdmission(ctx context.Context) error {
	admission, found, err := p.readNewsAdmission(ctx)
	if err != nil {
		return err
	}
	if !found {
		if !p.retentionNeedsReconciliation {
			return nil
		}
		if err := p.reconcileStoredNews(ctx); err != nil {
			return err
		}
		p.retentionNeedsReconciliation = false

		return nil
	}
	if err := p.reconcileStoredNews(ctx); err != nil {
		p.retentionNeedsReconciliation = true

		return fmt.Errorf("reconcile pending news admission: %w", err)
	}
	if err := p.removeNewsIdentity(ctx, admission.record.ID()); err != nil {
		p.retentionNeedsReconciliation = true

		return fmt.Errorf("reset pending news admission: %w", err)
	}
	if err := p.reconcileStoredNews(ctx); err != nil {
		p.retentionNeedsReconciliation = true

		return fmt.Errorf("reload pending news admission: %w", err)
	}
	p.applyRetentionLimits()
	now := p.now().UTC()
	if newsCreationAdmitted(admission.record.Created, now, admission.record.Category) {
		if _, err := p.applyNewsAdmission(
			ctx, admission.record, false, admission.destinations,
		); err != nil {
			p.retentionNeedsReconciliation = true

			return fmt.Errorf("replay pending news admission: %w", err)
		}
	}
	if err := p.clearNewsAdmission(ctx); err != nil {
		p.retentionNeedsReconciliation = true

		return fmt.Errorf("finish pending news admission: %w", err)
	}
	p.retentionNeedsReconciliation = false

	return nil
}

func (p *Pool) reconcileStoredNews(ctx context.Context) error {
	if err := p.pruneWhileHoldingWritePermit(ctx); err != nil {
		return fmt.Errorf("prune stored news: %w", err)
	}
	if err := p.loadStoredState(ctx); err != nil {
		return fmt.Errorf("reload stored news: %w", err)
	}
	if err := p.clearCleanupCursors(ctx); err != nil {
		return fmt.Errorf("finish stored news cleanup: %w", err)
	}

	return nil
}

func (p *Pool) removeNewsIdentity(ctx context.Context, id string) error {
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := p.removeNewsIdentityQueueRows(tx, id); err != nil {
			return err
		}
		if err := p.forgetKnownNews(tx, vault.Key(id)); err != nil {
			return fmt.Errorf("forget pending news identity: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("remove news identity: %w", err)
	}

	return nil
}

func (p *Pool) removeNewsIdentityQueueRows(tx *vault.Txn, id string) error {
	var after vault.Key
	for {
		page, err := tx.ReadBucketKeyPage(queueBucket, after, newsScrubPage)
		if err != nil {
			return fmt.Errorf("read pending news queues: %w", err)
		}
		if err := p.removeNewsIdentityQueuePage(tx, page.Keys, id); err != nil {
			return err
		}
		if len(page.Keys) == 0 || !page.More {
			return nil
		}
		after = page.Keys[len(page.Keys)-1]
	}
}

func (p *Pool) removeNewsIdentityQueuePage(
	tx *vault.Txn,
	keys []vault.Key,
	id string,
) error {
	for _, key := range keys {
		wire, _, _, err := p.readQueuedNewsWire(tx, key)
		if err != nil {
			if errors.Is(err, vault.ErrCorruptValue) {
				continue
			}

			return fmt.Errorf("read pending news queue row: %w", err)
		}
		record, decoded := decodeQueuedNewsRecord(wire)
		if !decoded || record.ID() != id {
			continue
		}
		if _, err := p.queue.Delete(tx, key); err != nil {
			return fmt.Errorf("delete pending news queue row: %w", err)
		}
	}

	return nil
}
