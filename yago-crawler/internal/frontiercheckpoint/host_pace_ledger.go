package frontiercheckpoint

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
)

type hostPaceRecord struct {
	State    crawlpace.HostState `json:"state"`
	Sequence uint64              `json:"sequence"`
}

type hostPaceLedger struct {
	metadata *bolt.Bucket
	paces    *bolt.Bucket
	order    *bolt.Bucket
}

func (checkpoint *FrontierCheckpoint) HostPaces(
	ctx context.Context,
	capacity int,
) (map[string]crawlpace.HostState, error) {
	if capacity <= 0 {
		return nil, ErrInvalidHostState
	}
	var states map[string]crawlpace.HostState
	err := checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		var loadErr error
		states, loadErr = loadHostPaces(transaction, capacity)

		return loadErr
	})

	return states, err
}

func loadHostPaces(
	transaction *bolt.Tx,
	capacity int,
) (map[string]crawlpace.HostState, error) {
	metadata, err := schemaBucket(transaction, metadataBucket)
	if err != nil {
		return nil, err
	}
	paces, order, err := hostPaceBuckets(transaction)
	if err != nil {
		return nil, err
	}
	total, err := hostPaceTotal(metadata, paces)
	if err != nil {
		return nil, err
	}
	total, err = trimHostPaces(paces, order, total, capacity)
	if err != nil {
		return nil, err
	}
	if err := putRow(
		metadata,
		hostPaceTotalKey,
		sequenceValue(total),
		"host pace total",
	); err != nil {
		return nil, err
	}
	if err := validateHostPaceOrder(paces, order, total); err != nil {
		return nil, err
	}

	return collectHostPaces(paces)
}

func trimHostPaces(
	paces *bolt.Bucket,
	order *bolt.Bucket,
	total uint64,
	capacity int,
) (uint64, error) {
	overflow := total
	for remaining := capacity; remaining > 0 && overflow > 0; remaining-- {
		overflow--
	}
	for overflow > 0 {
		if err := deleteOldestHostPace(paces, order); err != nil {
			return 0, err
		}
		overflow--
		total--
	}

	return total, nil
}

func collectHostPaces(paces *bolt.Bucket) (map[string]crawlpace.HostState, error) {
	states := make(map[string]crawlpace.HostState)
	err := paces.ForEach(func(host, encoded []byte) error {
		record, err := decodeHostPaceRecord(encoded)
		if err != nil {
			return err
		}
		states[string(host)] = record.State

		return nil
	})
	if err != nil {
		return nil, wrapDatabaseError("iterate frontier checkpoint host paces", err)
	}

	return states, nil
}

func recordHostPace(
	transaction *bolt.Tx,
	host string,
	state crawlpace.HostState,
	capacity int,
) error {
	if strings.TrimSpace(host) == "" || capacity <= 0 || validateHostPaceState(state) != nil {
		return ErrInvalidHostState
	}
	metadata, err := schemaBucket(transaction, metadataBucket)
	if err != nil {
		return err
	}
	paces, order, err := hostPaceBuckets(transaction)
	if err != nil {
		return err
	}
	total, err := hostPaceTotal(metadata, paces)
	if err != nil {
		return err
	}
	hostKey := []byte(host)
	total, current, err := prepareHostPaceRecord(paces, order, hostKey, state, total)
	if err != nil || !current {
		return err
	}
	sequence, err := nextHostPaceSequence(metadata)
	if err != nil {
		return err
	}
	ledger := hostPaceLedger{metadata: metadata, paces: paces, order: order}
	if err := persistHostPaceRecord(ledger, hostKey, state, sequence); err != nil {
		return err
	}
	total, err = trimHostPaces(paces, order, total, capacity)
	if err != nil {
		return err
	}
	return putRow(metadata, hostPaceTotalKey, sequenceValue(total), "host pace total")
}

func prepareHostPaceRecord(
	paces *bolt.Bucket,
	order *bolt.Bucket,
	hostKey []byte,
	state crawlpace.HostState,
	total uint64,
) (uint64, bool, error) {
	existing := paces.Get(hostKey)
	if existing == nil {
		next, err := nextValue(total)

		return next, true, err
	}
	record, err := decodeHostPaceRecord(existing)
	if err != nil {
		return 0, false, err
	}
	if state.Generation < record.State.Generation {
		return total, false, nil
	}
	if state.Generation == record.State.Generation && !equalHostPaceState(state, record.State) {
		return 0, false, fmt.Errorf("%w: host pace generation conflict", ErrCorruptCheckpoint)
	}
	oldOrderKey := sequenceValue(record.Sequence)
	if owner := order.Get(oldOrderKey); !bytes.Equal(owner, hostKey) {
		return 0, false, fmt.Errorf("%w: host pace order mismatch", ErrCorruptCheckpoint)
	}
	if err := deleteRow(order, oldOrderKey, "host pace order"); err != nil {
		return 0, false, err
	}

	return total, true, nil
}

func persistHostPaceRecord(
	ledger hostPaceLedger,
	hostKey []byte,
	state crawlpace.HostState,
	sequence uint64,
) error {
	metadataSequence := sequenceValue(sequence)
	if err := putRow(
		ledger.metadata,
		hostPaceSequenceKey,
		metadataSequence,
		"host pace sequence",
	); err != nil {
		return err
	}
	encoded, err := encodeRow("host pace", hostPaceRecord{State: state, Sequence: sequence})
	if err != nil {
		return err
	}
	if err := putRow(ledger.paces, hostKey, encoded, "host pace"); err != nil {
		return err
	}

	return putRow(ledger.order, metadataSequence, hostKey, "host pace order")
}

func equalHostPaceState(first, second crawlpace.HostState) bool {
	return first.NextDueAt.Equal(second.NextDueAt) &&
		first.BackoffUntil.Equal(second.BackoffUntil) &&
		first.BackoffPenalty == second.BackoffPenalty &&
		first.BackoffFailures == second.BackoffFailures &&
		first.Generation == second.Generation
}

func hostPaceBuckets(transaction *bolt.Tx) (*bolt.Bucket, *bolt.Bucket, error) {
	paces, err := schemaBucket(transaction, hostPacesBucket)
	if err != nil {
		return nil, nil, err
	}
	order, err := schemaBucket(transaction, hostPaceOrderBucket)
	if err != nil {
		return nil, nil, err
	}

	return paces, order, nil
}

func nextHostPaceSequence(metadata *bolt.Bucket) (uint64, error) {
	encoded := metadata.Get(hostPaceSequenceKey)
	var current uint64
	if encoded != nil {
		if len(encoded) != 8 {
			return 0, fmt.Errorf("%w: invalid host pace sequence", ErrCorruptCheckpoint)
		}
		current = binary.BigEndian.Uint64(encoded)
	}
	return nextValue(current)
}

func hostPaceTotal(metadata, paces *bolt.Bucket) (uint64, error) {
	encoded := metadata.Get(hostPaceTotalKey)
	if encoded != nil {
		if len(encoded) != 8 {
			return 0, fmt.Errorf("%w: invalid host pace total", ErrCorruptCheckpoint)
		}
		return binary.BigEndian.Uint64(encoded), nil
	}
	var total uint64
	cursor := paces.Cursor()
	for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
		total++
	}
	return total, nil
}

func decodeHostPaceRecord(encoded []byte) (hostPaceRecord, error) {
	var record hostPaceRecord
	if err := decodeRow("host pace", encoded, &record); err != nil {
		return hostPaceRecord{}, err
	}
	if record.Sequence == 0 || validateHostPaceState(record.State) != nil {
		return hostPaceRecord{}, fmt.Errorf("%w: invalid host pace record", ErrCorruptCheckpoint)
	}

	return record, nil
}

func deleteOldestHostPace(paces, order *bolt.Bucket) error {
	sequence, host := order.Cursor().First()
	if len(sequence) != 8 || len(host) == 0 {
		return fmt.Errorf("%w: invalid host pace order", ErrCorruptCheckpoint)
	}
	record, err := decodeHostPaceRecord(paces.Get(host))
	if err != nil {
		return err
	}
	if record.Sequence != binary.BigEndian.Uint64(sequence) {
		return fmt.Errorf("%w: host pace sequence mismatch", ErrCorruptCheckpoint)
	}
	return errors.Join(
		deleteRow(paces, host, "host pace"),
		deleteRow(order, sequence, "host pace order"),
	)
}

func validateHostPaceOrder(paces, order *bolt.Bucket, total uint64) error {
	var paceRows uint64
	paceCursor := paces.Cursor()
	for key, _ := paceCursor.First(); key != nil; key, _ = paceCursor.Next() {
		paceRows++
	}
	var orderRows uint64
	orderCursor := order.Cursor()
	for key, _ := orderCursor.First(); key != nil; key, _ = orderCursor.Next() {
		orderRows++
	}
	if paceRows != total || orderRows != total {
		return fmt.Errorf("%w: host pace order size mismatch", ErrCorruptCheckpoint)
	}
	err := order.ForEach(func(sequence, host []byte) error {
		if len(sequence) != 8 || len(host) == 0 {
			return fmt.Errorf("%w: invalid host pace order", ErrCorruptCheckpoint)
		}
		record, err := decodeHostPaceRecord(paces.Get(host))
		if err != nil {
			return err
		}
		if record.Sequence != binary.BigEndian.Uint64(sequence) {
			return fmt.Errorf("%w: host pace sequence mismatch", ErrCorruptCheckpoint)
		}

		return nil
	})

	return wrapDatabaseError("validate frontier checkpoint host pace order", err)
}
