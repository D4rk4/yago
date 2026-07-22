package peernews

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	queueBucket         vault.Name = "peernews-queue"
	knownBucket         vault.Name = "peernews-known"
	knownCategoryBucket vault.Name = "peernews-known-category"
	cursorBucket        vault.Name = "peernews-cursor"
	cleanupBucket       vault.Name = "peernews-cleanup"

	distributionLimit = 30
)

type Queue string

const (
	Incoming  Queue = "incoming"
	Processed Queue = "processed"
	Outgoing  Queue = "outgoing"
	Published Queue = "published"
)

const knownMarker = "1"

type wireCodec struct{}

func (wireCodec) Encode(value string) ([]byte, error) { return []byte(value), nil }
func (wireCodec) Decode(raw []byte) (string, error)   { return string(raw), nil }

type knownCodec struct{}

func (knownCodec) Encode(value string) ([]byte, error) {
	if value != knownMarker {
		return nil, fmt.Errorf("%w: known news marker %q", ErrBadNewsRecord, value)
	}

	return []byte(knownMarker), nil
}

func (knownCodec) Decode(raw []byte) (string, error) {
	if string(raw) != knownMarker {
		return "", fmt.Errorf("%w: known news marker %q", ErrBadNewsRecord, raw)
	}

	return knownMarker, nil
}

type cursorCodec struct{}

func (cursorCodec) Encode(value uint64) ([]byte, error) {
	return []byte(strconv.FormatUint(value, 10)), nil
}

func (cursorCodec) Decode(raw []byte) (uint64, error) {
	value, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: news cursor %q: %w", ErrBadNewsRecord, raw, err)
	}

	return value, nil
}

type Pool struct {
	writePermit                  newsWritePermit
	queue                        *vault.Collection[string]
	known                        *vault.Collection[string]
	knownCategories              *vault.Keyspace[string]
	cursor                       *vault.Collection[uint64]
	cleanup                      *vault.Keyspace[string]
	vault                        *vault.Vault
	now                          func() time.Time
	attachment                   seedAttachment
	retention                    newsRetention
	stored                       newsStoredState
	knownNewsRetention           *boundedNewestNews
	queuedNewsRetention          *boundedNewestNews
	retentionNeedsReconciliation bool
}

func Open(v *vault.Vault, now func() time.Time) (*Pool, error) {
	queue, err := vault.Register(v, queueBucket, wireCodec{})
	if err != nil {
		return nil, fmt.Errorf("register news queue: %w", err)
	}
	known, err := vault.Register(v, knownBucket, knownCodec{})
	if err != nil {
		return nil, fmt.Errorf("register known news: %w", err)
	}
	knownCategories, err := vault.RegisterKeyspace(v, knownCategoryBucket, knownCategoryCodec{})
	if err != nil {
		return nil, fmt.Errorf("register known news categories: %w", err)
	}
	cursor, err := vault.Register(v, cursorBucket, cursorCodec{})
	if err != nil {
		return nil, fmt.Errorf("register news cursor: %w", err)
	}
	cleanup, err := vault.RegisterKeyspace(v, cleanupBucket, newsCleanupCodec{})
	if err != nil {
		return nil, fmt.Errorf("register news cleanup: %w", err)
	}

	pool := &Pool{
		writePermit: newNewsWritePermit(),
		queue:       queue, known: known, knownCategories: knownCategories,
		cursor: cursor, cleanup: cleanup, vault: v, now: now,
		retention: newsRetention{
			queueRecords: maximumNewsQueueRecords,
			queueBytes:   maximumNewsQueueBytes,
			knownRecords: maximumKnownNewsRecords,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), newsRetentionTimeout)
	defer cancel()
	if err := pool.prune(ctx); err != nil {
		return nil, fmt.Errorf("prune peer news: %w", err)
	}
	if err := pool.loadStoredState(ctx); err != nil {
		return nil, fmt.Errorf("load peer news retention: %w", err)
	}
	if err := pool.recoverNewsRotation(ctx); err != nil {
		return nil, fmt.Errorf("recover peer news publication: %w", err)
	}
	if err := pool.recoverNewsAdmission(ctx); err != nil {
		return nil, fmt.Errorf("recover peer news admission: %w", err)
	}
	if err := pool.clearCleanupCursors(ctx); err != nil {
		return nil, fmt.Errorf("finish peer news cleanup: %w", err)
	}

	return pool, nil
}

func (p *Pool) PublishOwnNews(
	ctx context.Context,
	originator yagomodel.Hash,
	category string,
	attributes map[string]string,
) error {
	if err := p.writePermit.Acquire(ctx); err != nil {
		return fmt.Errorf("publish own news: %w", err)
	}
	defer p.writePermit.Release()

	if len(category) > categoryMaxLength {
		return fmt.Errorf("%w: category %q too long", ErrBadNewsRecord, category)
	}
	now := p.now().UTC().Truncate(time.Second)
	record := Record{
		Originator: originator,
		Created:    now,
		Category:   category,
		Attributes: map[string]string{},
	}
	for key, value := range attributes {
		if key == attributeIDOffset {
			if offset, err := strconv.Atoi(value); err == nil {
				record.Created = record.Created.Add(time.Duration(offset) * time.Second)
			}
			continue
		}
		record.Attributes[key] = value
	}
	if !newsRecordAdmitted(record) || !newsPublicationAdmitted(record) {
		return fmt.Errorf("%w: record exceeds %d bytes", ErrBadNewsRecord, maximumNewsRecordBytes)
	}

	_, err := p.storeNewsRecord(ctx, record, now, []Queue{Incoming, Outgoing})
	if err != nil {
		return fmt.Errorf("publish own news: %w", err)
	}

	return nil
}

func (p *Pool) NextPublication(ctx context.Context) (Record, bool, error) {
	if err := p.writePermit.Acquire(ctx); err != nil {
		return Record{}, false, fmt.Errorf("next news publication: %w", err)
	}
	defer p.writePermit.Release()
	if err := p.recoverNewsRotation(ctx); err != nil {
		return Record{}, false, fmt.Errorf("next news publication: %w", err)
	}
	if err := p.recoverNewsAdmission(ctx); err != nil {
		return Record{}, false, fmt.Errorf("next news publication: %w", err)
	}

	now := p.now().UTC()
	for {
		if err := ctx.Err(); err != nil {
			return Record{}, false, fmt.Errorf("next news publication: %w", err)
		}
		key, record, found, valid, err := p.nextPublicationCandidate(ctx, now)
		if err != nil {
			return Record{}, false, fmt.Errorf("next news publication: %w", err)
		}
		if !found {
			return Record{}, false, nil
		}
		record, found, err = p.rotatePublication(ctx, key, record, valid)
		if err != nil {
			return Record{}, false, fmt.Errorf("next news publication: %w", err)
		}
		if found {
			return record, true, nil
		}
	}
}

func (p *Pool) EnqueueIncomingNews(ctx context.Context, record Record) (bool, error) {
	if err := p.writePermit.Acquire(ctx); err != nil {
		return false, fmt.Errorf("enqueue incoming news: %w", err)
	}
	defer p.writePermit.Release()

	now := p.now().UTC()
	if record.Created.IsZero() || !knownNewsCategories[record.Category] ||
		!newsRecordAdmitted(record) ||
		!newsCreationAdmitted(record.Created, now, record.Category) {
		return false, nil
	}

	stored, err := p.storeNewsRecord(ctx, record, now, []Queue{Incoming})
	if err != nil {
		return false, fmt.Errorf("enqueue incoming news: %w", err)
	}

	return stored, nil
}

func (p *Pool) ByID(ctx context.Context, queue Queue, id string) (Record, bool, error) {
	var (
		record Record
		found  bool
	)
	now := p.now().UTC()
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		return p.queue.Scan(tx, queuePrefix(queue), func(_ vault.Key, wire string) (bool, error) {
			candidate, err := parseRecord(wire, time.Time{})
			if err != nil {
				return false, fmt.Errorf("stored news record: %w", err)
			}
			if candidate.ID() != id {
				return true, nil
			}
			matches, err := p.knownRecordMatches(tx, candidate)
			if err != nil {
				return false, err
			}
			if !newsCreationAdmitted(candidate.Created, now, candidate.Category) || !matches {
				return true, nil
			}
			record = candidate
			found = true

			return false, nil
		})
	})
	if err != nil {
		return Record{}, false, fmt.Errorf("news by id: %w", err)
	}

	return record, found, nil
}

// Recent returns up to limit records from a queue, newest first. It is a
// read-only view for the admin console and does not touch the distribution state.
func (p *Pool) Recent(ctx context.Context, queue Queue, limit int) ([]Record, error) {
	if limit <= 0 {
		return nil, nil
	}

	var records []Record
	now := p.now().UTC()
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		return p.queue.Scan(tx, queuePrefix(queue), func(_ vault.Key, wire string) (bool, error) {
			record, err := parseRecord(wire, time.Time{})
			if err != nil {
				return false, fmt.Errorf("stored news record: %w", err)
			}
			matches, err := p.knownRecordMatches(tx, record)
			if err != nil {
				return false, err
			}
			if !newsCreationAdmitted(record.Created, now, record.Category) || !matches {
				return true, nil
			}
			records = append(records, record)

			return true, nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("recent %s news: %w", queue, err)
	}

	slices.SortFunc(records, func(left, right Record) int {
		if comparison := right.Created.Compare(left.Created); comparison != 0 {
			return comparison
		}

		return strings.Compare(left.ID(), right.ID())
	})
	if len(records) > limit {
		records = records[:limit]
	}

	return records, nil
}

func (p *Pool) push(tx *vault.Txn, queue Queue, record Record) (vault.Key, error) {
	sequence, err := p.nextSequence(tx, queue)
	if err != nil {
		return nil, err
	}
	key := queueKey(queue, sequence)
	if err := p.queue.Put(tx, key, record.WireForm()); err != nil {
		return nil, fmt.Errorf("push %s news: %w", queue, err)
	}

	return key, nil
}

func (p *Pool) nextSequence(tx *vault.Txn, queue Queue) (uint64, error) {
	sequence, err := p.storedQueueSequence(tx, queue)
	if err != nil {
		return 0, fmt.Errorf("read %s news cursor: %w", queue, err)
	}
	if sequence == math.MaxUint64 {
		return 0, fmt.Errorf("%s news cursor exhausted", queue)
	}
	sequence++
	if err := p.cursor.Put(tx, vault.Key(queue), sequence); err != nil {
		return 0, fmt.Errorf("advance %s news cursor: %w", queue, err)
	}

	return sequence, nil
}

func (p *Pool) storedQueueSequence(
	tx *vault.Txn,
	queue Queue,
) (uint64, error) {
	key := vault.Key(queue)
	size, found, err := p.cursor.EncodedSize(tx, key)
	if err != nil {
		return 0, fmt.Errorf("inspect %s news cursor: %w", queue, err)
	}
	if !found {
		return 0, nil
	}
	if size > 20 {
		return 0, fmt.Errorf(
			"%w: %w: %s news cursor size %d",
			vault.ErrCorruptValue,
			ErrBadNewsRecord,
			queue,
			size,
		)
	}

	sequence, present, err := p.cursor.Get(tx, key)
	if err != nil {
		return 0, fmt.Errorf("read %s news cursor: %w", queue, err)
	}
	if !present {
		return 0, fmt.Errorf(
			"%w: %s news cursor disappeared during read",
			vault.ErrCorruptValue,
			queue,
		)
	}

	return sequence, nil
}

func queuePrefix(queue Queue) vault.Key {
	return vault.Key(string(queue) + "/")
}

func queueKey(queue Queue, sequence uint64) vault.Key {
	var suffix [8]byte
	binary.BigEndian.PutUint64(suffix[:], sequence)

	return append(queuePrefix(queue), suffix[:]...)
}
