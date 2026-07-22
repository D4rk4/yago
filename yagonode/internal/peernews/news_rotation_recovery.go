package peernews

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var newsRotationKey = vault.Key("rotation")

type newsRotation struct {
	source      vault.Key
	original    Record
	rotated     Record
	destination Queue
}

type storedNewsRotation struct {
	Source   []byte `json:"source"`
	Original []byte `json:"original"`
}

func (p *Pool) storeNewsRotation(ctx context.Context, rotation newsRotation) error {
	raw, _ := json.Marshal(storedNewsRotation{
		Source: rotation.source, Original: []byte(rotation.original.WireForm()),
	})
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := p.cleanup.Put(tx, newsRotationKey, string(raw)); err != nil {
			return fmt.Errorf("store news rotation: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update news rotation: %w", err)
	}

	return nil
}

func (p *Pool) readNewsRotation(ctx context.Context) (newsRotation, bool, error) {
	var rotation newsRotation
	found := false
	invalid := false
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		raw, present, err := p.storedNewsCleanup(tx, newsRotationKey)
		if err != nil {
			return err
		}
		if !present {
			return nil
		}
		decoded, valid := decodeNewsRotation(raw)
		if !valid {
			return fmt.Errorf("%w: invalid news rotation", vault.ErrCorruptValue)
		}
		rotation = decoded
		found = true

		return nil
	})
	if err != nil {
		if !errors.Is(err, vault.ErrCorruptValue) {
			return newsRotation{}, false, fmt.Errorf("read news rotation: %w", err)
		}
		invalid = true
	}
	if invalid {
		if err := p.clearNewsRotation(ctx); err != nil {
			return newsRotation{}, false, fmt.Errorf("discard invalid news rotation: %w", err)
		}
	}

	return rotation, found, nil
}

func decodeNewsRotation(raw string) (newsRotation, bool) {
	var stored storedNewsRotation
	if json.Unmarshal([]byte(raw), &stored) != nil {
		return newsRotation{}, false
	}
	queue, validSource := parseQueueKey(stored.Source)
	original, originalErr := parseRecord(string(stored.Original), time.Time{})
	rotated := original
	rotated.Distributed++
	destination := Outgoing
	if rotated.Distributed >= distributionLimit {
		destination = Published
	}
	valid := validSource && queue == Outgoing && originalErr == nil
	if !valid {
		return newsRotation{}, false
	}

	return newsRotation{
		source: stored.Source, original: original,
		rotated: rotated, destination: destination,
	}, true
}

func (p *Pool) clearNewsRotation(ctx context.Context) error {
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := p.cleanup.Delete(tx, newsRotationKey); err != nil {
			return fmt.Errorf("clear news rotation: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update cleared news rotation: %w", err)
	}

	return nil
}

func (p *Pool) recoverNewsRotation(ctx context.Context) error {
	rotation, found, err := p.readNewsRotation(ctx)
	if err != nil || !found {
		return err
	}
	if err := p.rollbackNewsRotation(ctx, rotation); err != nil {
		p.retentionNeedsReconciliation = true

		return fmt.Errorf("rollback pending news rotation: %w", err)
	}
	if err := p.reconcileStoredNews(ctx); err != nil {
		p.retentionNeedsReconciliation = true

		return fmt.Errorf("reconcile pending news rotation: %w", err)
	}
	if err := p.clearNewsRotation(ctx); err != nil {
		p.retentionNeedsReconciliation = true

		return fmt.Errorf("finish pending news rotation: %w", err)
	}
	p.retentionNeedsReconciliation = false

	return nil
}

func (p *Pool) rollbackNewsRotation(ctx context.Context, rotation newsRotation) error {
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		page, err := tx.ReadBucketKeyPage(queueBucket, nil, maximumNewsQueueRecords+1)
		if err != nil {
			return fmt.Errorf("read pending news rotation: %w", err)
		}
		if err := p.removeNewsRotationRows(tx, page.Keys, rotation); err != nil {
			return err
		}
		if page.More {
			return fmt.Errorf("pending news rotation exceeds retention bound")
		}
		if err := p.queue.Put(tx, rotation.source, rotation.original.WireForm()); err != nil {
			return fmt.Errorf("restore pending news rotation: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("rollback news rotation: %w", err)
	}

	return nil
}

func (p *Pool) removeNewsRotationRows(
	tx *vault.Txn,
	keys []vault.Key,
	rotation newsRotation,
) error {
	for _, key := range keys {
		matches, err := p.newsRotationRowMatches(tx, key, rotation)
		if err != nil {
			return err
		}
		if !matches {
			continue
		}
		if _, err := p.queue.Delete(tx, key); err != nil {
			return fmt.Errorf("remove pending news rotation: %w", err)
		}
	}

	return nil
}

func (p *Pool) newsRotationRowMatches(
	tx *vault.Txn,
	key vault.Key,
	rotation newsRotation,
) (bool, error) {
	queue, valid := parseQueueKey(key)
	if !valid || (queue != Outgoing && queue != rotation.destination) ||
		bytes.Equal(key, rotation.source) {
		return false, nil
	}
	wire, _, _, err := p.readQueuedNewsWire(tx, key)
	if err != nil {
		if errors.Is(err, vault.ErrCorruptValue) {
			return false, nil
		}

		return false, fmt.Errorf("read pending news rotation row: %w", err)
	}
	return wire == rotation.rotated.WireForm() || wire == rotation.original.WireForm(), nil
}
