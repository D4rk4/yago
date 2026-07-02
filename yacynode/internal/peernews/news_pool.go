package peernews

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

const (
	queueBucket  vault.Name = "peernews-queue"
	knownBucket  vault.Name = "peernews-known"
	cursorBucket vault.Name = "peernews-cursor"

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

func (knownCodec) Encode(value string) ([]byte, error) { return []byte(value), nil }

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
	queue      *vault.Collection[string]
	known      *vault.Collection[string]
	cursor     *vault.Collection[uint64]
	vault      *vault.Vault
	now        func() time.Time
	attachment seedAttachment
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
	cursor, err := vault.Register(v, cursorBucket, cursorCodec{})
	if err != nil {
		return nil, fmt.Errorf("register news cursor: %w", err)
	}

	return &Pool{queue: queue, known: known, cursor: cursor, vault: v, now: now}, nil
}

func (p *Pool) PublishOwnNews(
	ctx context.Context,
	originator yacymodel.Hash,
	category string,
	attributes map[string]string,
) error {
	if len(category) > categoryMaxLength {
		return fmt.Errorf("%w: category %q too long", ErrBadNewsRecord, category)
	}
	record := Record{
		Originator: originator,
		Created:    p.now().UTC().Truncate(time.Second),
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

	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		_, exists, err := p.known.Get(tx, vault.Key(record.ID()))
		if err != nil {
			return fmt.Errorf("check known news: %w", err)
		}
		if exists {
			return nil
		}
		if err := p.known.Put(tx, vault.Key(record.ID()), knownMarker); err != nil {
			return fmt.Errorf("remember news: %w", err)
		}
		if err := p.push(tx, Incoming, record); err != nil {
			return err
		}

		return p.push(tx, Outgoing, record)
	})
	if err != nil {
		return fmt.Errorf("publish own news: %w", err)
	}

	return nil
}

func (p *Pool) NextPublication(ctx context.Context) (Record, bool, error) {
	var (
		record Record
		found  bool
	)
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		key, wire, ok, err := p.popHead(tx, Outgoing)
		if err != nil || !ok {
			return err
		}
		record, err = parseRecord(wire, time.Time{})
		if err != nil {
			return fmt.Errorf("stored news record %q: %w", key, err)
		}
		record.Distributed++
		found = true

		destination := Outgoing
		if record.Distributed >= distributionLimit {
			destination = Published
		}

		return p.push(tx, destination, record)
	})
	if err != nil {
		return Record{}, false, fmt.Errorf("next news publication: %w", err)
	}

	return record, found, nil
}

func (p *Pool) EnqueueIncomingNews(ctx context.Context, record Record) (bool, error) {
	if record.Created.IsZero() || !knownNewsCategories[record.Category] {
		return false, nil
	}

	stored := false
	err := p.vault.Update(ctx, func(tx *vault.Txn) error {
		_, exists, err := p.known.Get(tx, vault.Key(record.ID()))
		if err != nil {
			return fmt.Errorf("check known news: %w", err)
		}
		if exists {
			return nil
		}
		if err := p.known.Put(tx, vault.Key(record.ID()), knownMarker); err != nil {
			return fmt.Errorf("remember news: %w", err)
		}
		stored = true

		return p.push(tx, Incoming, record)
	})
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
	err := p.vault.View(ctx, func(tx *vault.Txn) error {
		return p.queue.Scan(tx, queuePrefix(queue), func(_ vault.Key, wire string) (bool, error) {
			candidate, err := parseRecord(wire, time.Time{})
			if err != nil {
				return false, fmt.Errorf("stored news record: %w", err)
			}
			if candidate.ID() != id {
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

func (p *Pool) push(tx *vault.Txn, queue Queue, record Record) error {
	sequence, err := p.nextSequence(tx, queue)
	if err != nil {
		return err
	}
	if err := p.queue.Put(tx, queueKey(queue, sequence), record.WireForm()); err != nil {
		return fmt.Errorf("push %s news: %w", queue, err)
	}

	return nil
}

func (p *Pool) popHead(tx *vault.Txn, queue Queue) (vault.Key, string, bool, error) {
	var (
		key  vault.Key
		wire string
		ok   bool
	)
	err := p.queue.Scan(tx, queuePrefix(queue), func(k vault.Key, value string) (bool, error) {
		key = append(vault.Key(nil), k...)
		wire = value
		ok = true

		return false, nil
	})
	if err != nil {
		return nil, "", false, fmt.Errorf("scan %s news: %w", queue, err)
	}
	if !ok {
		return nil, "", false, nil
	}
	if _, err := p.queue.Delete(tx, key); err != nil {
		return nil, "", false, fmt.Errorf("pop %s news: %w", queue, err)
	}

	return key, wire, true, nil
}

func (p *Pool) nextSequence(tx *vault.Txn, queue Queue) (uint64, error) {
	sequence, _, err := p.cursor.Get(tx, vault.Key(queue))
	if err != nil {
		return 0, fmt.Errorf("read %s news cursor: %w", queue, err)
	}
	sequence++
	if err := p.cursor.Put(tx, vault.Key(queue), sequence); err != nil {
		return 0, fmt.Errorf("advance %s news cursor: %w", queue, err)
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
