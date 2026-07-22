package peernews

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type queuedNewsEvidence struct {
	category   string
	generation string
	key        vault.Key
	priority   int
}

type queuedNewsEvidenceCatalog map[string]queuedNewsEvidence

type queuedNewsCatalog struct {
	evidence       queuedNewsEvidenceCatalog
	latestSequence map[Queue]uint64
}

func (p *Pool) buildQueuedNewsEvidenceCatalog(
	ctx context.Context,
	now time.Time,
) (queuedNewsCatalog, error) {
	catalog := queuedNewsCatalog{
		evidence:       make(queuedNewsEvidenceCatalog, p.retention.knownRecords),
		latestSequence: make(map[Queue]uint64, len(newsQueues)),
	}
	var after vault.Key
	for {
		var page vault.BucketKeyPage
		if err := p.vault.View(ctx, func(tx *vault.Txn) error {
			var err error
			page, err = tx.ReadBucketKeyPage(queueBucket, after, newsScrubPage)
			if err != nil {
				return fmt.Errorf("read queued news evidence page: %w", err)
			}
			for _, key := range page.Keys {
				if err := p.catalogQueuedNewsEvidence(tx, key, now, &catalog); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			return queuedNewsCatalog{}, fmt.Errorf("build queued news evidence: %w", err)
		}
		if len(page.Keys) == 0 || !page.More {
			return catalog, nil
		}
		after = append(vault.Key(nil), page.Keys[len(page.Keys)-1]...)
	}
}

func (p *Pool) catalogQueuedNewsEvidence(
	tx *vault.Txn,
	key vault.Key,
	now time.Time,
	catalog *queuedNewsCatalog,
) error {
	queue, valid := parseQueueKey(key)
	if !valid {
		return nil
	}
	sequence := binary.BigEndian.Uint64(key[len(key)-8:])
	catalog.latestSequence[queue] = max(catalog.latestSequence[queue], sequence)
	identity, candidate, selected, err := p.queuedNewsEvidenceCandidate(tx, key, now)
	if err != nil || !selected {
		return err
	}
	current, found := catalog.evidence[identity]
	if !found || candidate.priority > current.priority ||
		candidate.priority == current.priority && bytes.Compare(candidate.key, current.key) < 0 {
		if !found && len(catalog.evidence) >= p.retention.knownRecords {
			return nil
		}
		catalog.evidence[identity] = candidate
	}

	return nil
}

func (p *Pool) raiseQueuedNewsCursors(
	ctx context.Context,
	latest map[Queue]uint64,
) error {
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, queue := range newsQueues {
			sequence, err := p.storedQueueSequence(tx, queue)
			if err != nil {
				if !errors.Is(err, vault.ErrCorruptValue) {
					return fmt.Errorf("read %s news cursor floor: %w", queue, err)
				}
				if err := p.cursor.Put(tx, vault.Key(queue), latest[queue]); err != nil {
					return fmt.Errorf("repair %s news cursor floor: %w", queue, err)
				}
				continue
			}
			if sequence >= latest[queue] {
				continue
			}
			if err := p.cursor.Put(tx, vault.Key(queue), latest[queue]); err != nil {
				return fmt.Errorf("raise %s news cursor floor: %w", queue, err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("update queued news cursors: %w", err)
	}

	return nil
}

func queuedNewsMatchesEvidence(record Record, evidence queuedNewsEvidence) bool {
	return record.Category == evidence.category &&
		knownCategoryGeneration(record) == evidence.generation
}
