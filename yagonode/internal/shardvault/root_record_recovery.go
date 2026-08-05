package shardvault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	rootRecordEnvelopeVersion  = byte(1)
	rootRecordEnvelopeHeader   = 9
	rootRecordQuarantineBucket = "__root_record_quarantine__"
	rootRecordRecoveryMessage  = "vault root record recovery completed"
)

var errStructuralRootRecord = errors.New("vault structural root contains a record")

var (
	deleteStructuralRootRecord  = (*bolt.Cursor).Delete
	createRootRecordQuarantine  = (*bolt.Tx).CreateBucketIfNotExists
	storeRootRecordQuarantine   = (*bolt.Bucket).Put
	createRootRecordDestination = (*bolt.Tx).CreateBucketIfNotExists
	storeRecoveredRootRecord    = (*bolt.Bucket).Put
	deleteRootRecordQuarantine  = (*bolt.Bucket).Delete
)

type rootRecordRecovery struct {
	destination vault.Name
	accept      func(vault.Key, []byte) bool
}

type rootRecordRecoveryStats struct {
	isolationCount      uint64
	restoredCount       uint64
	alreadyPresentCount uint64
	conflictCount       uint64
	retainedCount       uint64
}

type rootRecord struct {
	key    []byte
	stored []byte
}

type quarantinedRootRecord struct {
	identity []byte
	envelope []byte
	key      vault.Key
	stored   []byte
}

func WithRootRecordRecovery(
	destination vault.Name,
	accept func(vault.Key, []byte) bool,
) Option {
	return func(engine *engine) {
		engine.rootRecordRecovery = &rootRecordRecovery{
			destination: destination,
			accept:      accept,
		}
	}
}

func (engine *engine) recoverRootRecordsLocked(
	ctx context.Context,
	logger *slog.Logger,
) error {
	stats := rootRecordRecoveryStats{}
	isolationCount, err := engine.isolateShardRootRecords()
	if err != nil {
		return err
	}
	stats.isolationCount = isolationCount
	groups, retainedCount, err := engine.groupQuarantinedRootRecords()
	if err != nil {
		return err
	}
	stats.retainedCount = retainedCount
	if err := engine.replayRootRecordGroups(ctx, groups, &stats); err != nil {
		return err
	}
	if err := engine.releaseResolvedRootRecords(); err != nil {
		return err
	}
	logRootRecordRecovery(logger, stats)

	return nil
}

func (engine *engine) isolateShardRootRecords() (uint64, error) {
	var isolationCount uint64
	for shard, database := range engine.shards {
		isolated, err := isolateRootRecords(database)
		if err != nil {
			return 0, fmt.Errorf("isolate shard %d root records: %w", shard+1, err)
		}
		isolationCount += isolated
	}

	return isolationCount, nil
}

func (engine *engine) groupQuarantinedRootRecords() (
	[][]*quarantinedRootRecord,
	uint64,
	error,
) {
	groups := make([][]*quarantinedRootRecord, len(engine.shards))
	var retainedCount uint64
	for shard, database := range engine.shards {
		records, err := readQuarantinedRootRecords(database)
		if err != nil {
			return nil, 0, fmt.Errorf("read shard %d root quarantine: %w", shard+1, err)
		}
		for index := range records {
			record := &records[index]
			if !engine.acceptsRootRecord(record) {
				retainedCount++
				continue
			}
			destination := engine.route(engine.rootRecordRecovery.destination, record.key)
			groups[destination] = append(groups[destination], record)
		}
	}

	return groups, retainedCount, nil
}

func (engine *engine) replayRootRecordGroups(
	ctx context.Context,
	groups [][]*quarantinedRootRecord,
	stats *rootRecordRecoveryStats,
) error {
	for destination, records := range groups {
		if len(records) == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context: %w", err)
		}
		restored, alreadyPresent, conflicts, err := engine.replayRootRecords(
			engine.shards[destination],
			records,
		)
		if err != nil {
			return fmt.Errorf("replay root records to shard %d: %w", destination+1, err)
		}
		stats.restoredCount += restored
		stats.alreadyPresentCount += alreadyPresent
		stats.conflictCount += conflicts
		stats.retainedCount += conflicts
		if restored > 0 {
			engine.noteRecoveredWordKeys(records)
		}
	}

	return nil
}

func isolateRootRecords(database *bolt.DB) (uint64, error) {
	records, err := readRootRecords(database)
	if err != nil || len(records) == 0 {
		return 0, err
	}
	err = database.Update(func(tx *bolt.Tx) error {
		current := rootRecords(tx)
		cursor := tx.Cursor()
		for _, record := range current {
			cursor.Seek(record.key)
			if deleteErr := deleteStructuralRootRecord(cursor); deleteErr != nil {
				return fmt.Errorf("delete structural root record: %w", deleteErr)
			}
		}
		quarantine, createErr := createRootRecordQuarantine(
			tx,
			[]byte(rootRecordQuarantineBucket),
		)
		if createErr != nil {
			return fmt.Errorf("create root record quarantine: %w", createErr)
		}
		for _, record := range current {
			envelope := encodeRootRecordEnvelope(record)
			identity := sha256.Sum256(envelope)
			if existing := quarantine.Get(identity[:]); existing != nil &&
				!bytes.Equal(existing, envelope) {
				return errors.New("root record quarantine identity collision")
			}
			if putErr := storeRootRecordQuarantine(
				quarantine,
				identity[:],
				envelope,
			); putErr != nil {
				return fmt.Errorf("quarantine root record: %w", putErr)
			}
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("commit structural root isolation: %w", err)
	}
	if err := syncRecoveryDatabase(database); err != nil {
		return 0, err
	}

	return uint64(len(records)), nil
}

func readRootRecords(database *bolt.DB) ([]rootRecord, error) {
	var records []rootRecord
	if err := database.View(func(tx *bolt.Tx) error {
		records = rootRecords(tx)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("read structural root: %w", err)
	}

	return records, nil
}

func rootRecords(tx *bolt.Tx) []rootRecord {
	cursor := tx.Cursor()
	records := make([]rootRecord, 0)
	for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
		if tx.Bucket(key) != nil {
			continue
		}
		records = append(records, rootRecord{
			key:    append([]byte(nil), key...),
			stored: append([]byte(nil), value...),
		})
	}

	return records
}

func encodeRootRecordEnvelope(record rootRecord) []byte {
	envelope := make([]byte, rootRecordEnvelopeHeader+len(record.key)+len(record.stored))
	envelope[0] = rootRecordEnvelopeVersion
	binary.BigEndian.PutUint64(envelope[1:rootRecordEnvelopeHeader], uint64(len(record.key)))
	copy(envelope[rootRecordEnvelopeHeader:], record.key)
	copy(envelope[rootRecordEnvelopeHeader+len(record.key):], record.stored)

	return envelope
}

func readQuarantinedRootRecords(
	database *bolt.DB,
) ([]quarantinedRootRecord, error) {
	records := make([]quarantinedRootRecord, 0)
	err := database.View(func(tx *bolt.Tx) error {
		quarantine := tx.Bucket([]byte(rootRecordQuarantineBucket))
		if quarantine == nil {
			return nil
		}

		return quarantine.ForEach(func(identity, envelope []byte) error {
			records = append(records, retainedRootRecord(identity, envelope))

			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("scan root record quarantine: %w", err)
	}

	return records, nil
}

func retainedRootRecord(identity, envelope []byte) quarantinedRootRecord {
	record, err := decodeRootRecordEnvelope(identity, envelope)
	if err == nil {
		return record
	}

	return quarantinedRootRecord{
		identity: append([]byte(nil), identity...),
		envelope: append([]byte(nil), envelope...),
	}
}

func (engine *engine) acceptsRootRecord(record *quarantinedRootRecord) bool {
	if engine.rootRecordRecovery == nil || engine.rootRecordRecovery.accept == nil {
		return false
	}
	decoded, err := decodeValue(record.stored)

	return err == nil && engine.rootRecordRecovery.accept(record.key, decoded)
}

func decodeRootRecordEnvelope(identity, envelope []byte) (quarantinedRootRecord, error) {
	record := quarantinedRootRecord{
		identity: append([]byte(nil), identity...),
		envelope: append([]byte(nil), envelope...),
	}
	if len(identity) != sha256.Size || len(envelope) < rootRecordEnvelopeHeader ||
		envelope[0] != rootRecordEnvelopeVersion {
		return record, errors.New("invalid root record quarantine envelope")
	}
	sum := sha256.Sum256(envelope)
	if !bytes.Equal(identity, sum[:]) {
		return record, errors.New("root record quarantine checksum mismatch")
	}
	keyLength := binary.BigEndian.Uint64(envelope[1:rootRecordEnvelopeHeader])
	body := envelope[rootRecordEnvelopeHeader:]
	if keyLength == 0 || keyLength > uint64(len(body)) {
		return record, errors.New("invalid quarantined root record key length")
	}
	record.key = append(vault.Key(nil), body[:keyLength]...)
	record.stored = append([]byte(nil), body[keyLength:]...)

	return record, nil
}

func (engine *engine) replayRootRecords(
	database *bolt.DB,
	records []*quarantinedRootRecord,
) (uint64, uint64, uint64, error) {
	var restored uint64
	var alreadyPresent uint64
	err := database.Update(func(tx *bolt.Tx) error {
		destination, createErr := createRootRecordDestination(
			tx,
			[]byte(engine.rootRecordRecovery.destination),
		)
		if createErr != nil {
			return fmt.Errorf("create recovery destination: %w", createErr)
		}
		for _, record := range records {
			existing := destination.Get(record.key)
			switch {
			case existing == nil:
				if putErr := storeRecoveredRootRecord(
					destination,
					record.key,
					record.stored,
				); putErr != nil {
					return fmt.Errorf("restore quarantined root record: %w", putErr)
				}
				restored++
			case bytes.Equal(existing, record.stored):
				alreadyPresent++
			}
		}
		if err := addRecoveredCollectionLength(
			tx,
			engine.rootRecordRecovery.destination,
			restored,
		); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("commit recovered root records: %w", err)
	}
	if err := syncRecoveryDatabase(database); err != nil {
		return 0, 0, 0, err
	}
	return restored, alreadyPresent, uint64(len(records)) - restored - alreadyPresent, nil
}

func addRecoveredCollectionLength(tx *bolt.Tx, collection vault.Name, count uint64) error {
	if count == 0 {
		return nil
	}

	return applyCollectionLengthChanges(tx, collection, count, 0)
}

func (engine *engine) releaseResolvedRootRecords() error {
	for shard, database := range engine.shards {
		records, err := readQuarantinedRootRecords(database)
		if err != nil {
			return fmt.Errorf("read shard %d resolved root records: %w", shard+1, err)
		}
		resolved, err := engine.resolvedRootRecords(records)
		if err != nil {
			return err
		}
		if len(resolved) == 0 {
			continue
		}
		if err := deleteResolvedRootRecords(database, resolved); err != nil {
			return fmt.Errorf("release shard %d root records: %w", shard+1, err)
		}
	}

	return nil
}

func (engine *engine) resolvedRootRecords(
	records []quarantinedRootRecord,
) ([]quarantinedRootRecord, error) {
	resolved := make([]quarantinedRootRecord, 0)
	for index := range records {
		record := records[index]
		exact, err := engine.recoveredRootRecordIsCurrent(&record)
		if err != nil {
			return nil, err
		}
		if exact {
			resolved = append(resolved, record)
		}
	}

	return resolved, nil
}

func (engine *engine) recoveredRootRecordIsCurrent(
	record *quarantinedRootRecord,
) (bool, error) {
	if !engine.acceptsRootRecord(record) {
		return false, nil
	}
	destination := engine.route(engine.rootRecordRecovery.destination, record.key)
	var exact bool
	if err := engine.shards[destination].View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(engine.rootRecordRecovery.destination))
		exact = bucket != nil && bytes.Equal(bucket.Get(record.key), record.stored)

		return nil
	}); err != nil {
		return false, fmt.Errorf("verify recovered root record: %w", err)
	}

	return exact, nil
}

func deleteResolvedRootRecords(
	database *bolt.DB,
	records []quarantinedRootRecord,
) error {
	err := database.Update(func(tx *bolt.Tx) error {
		quarantine := tx.Bucket([]byte(rootRecordQuarantineBucket))
		if quarantine == nil {
			return nil
		}
		for _, record := range records {
			existing := quarantine.Get(record.identity)
			if existing == nil {
				continue
			}
			if !bytes.Equal(existing, record.envelope) {
				return errors.New("root record quarantine changed before release")
			}
			if err := deleteRootRecordQuarantine(
				quarantine,
				record.identity,
			); err != nil {
				return fmt.Errorf("delete recovered root record: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("commit root record release: %w", err)
	}

	return syncRecoveryDatabase(database)
}

func syncRecoveryDatabase(database *bolt.DB) error {
	if !database.NoSync {
		return nil
	}
	if err := syncDB(database); err != nil {
		return fmt.Errorf("sync recovered root records: %w", err)
	}

	return nil
}

func (engine *engine) noteRecoveredWordKeys(records []*quarantinedRootRecord) {
	if len(engine.wordFilters) != len(engine.shards) {
		return
	}
	for _, record := range records {
		engine.noteWordKey(engine.rootRecordRecovery.destination, record.key)
	}
}

func logRootRecordRecovery(logger *slog.Logger, stats rootRecordRecoveryStats) {
	if stats == (rootRecordRecoveryStats{}) {
		return
	}
	logger.LogAttrs(
		context.Background(),
		slog.LevelWarn,
		rootRecordRecoveryMessage,
		slog.Uint64("isolated", stats.isolationCount),
		slog.Uint64("restored", stats.restoredCount),
		slog.Uint64("alreadyPresent", stats.alreadyPresentCount),
		slog.Uint64("conflicts", stats.conflictCount),
		slog.Uint64("retained", stats.retainedCount),
	)
}

func validateStructuralRoot(database *bolt.DB) error {
	if err := database.View(validateStructuralRootTransaction); err != nil {
		return fmt.Errorf("validate structural root: %w", err)
	}

	return nil
}

func validateStructuralRootTransaction(tx *bolt.Tx) error {
	cursor := tx.Cursor()
	for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
		if tx.Bucket(key) == nil {
			return fmt.Errorf("%w: key %x", errStructuralRootRecord, key)
		}
	}

	return nil
}
