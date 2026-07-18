package frontiercheckpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
)

const terminalSettlementReconciliationBatchSize = 64

func (checkpoint *FrontierCheckpoint) Stage(
	ctx context.Context,
	settlement crawlsettlement.Settlement,
) error {
	settlement = crawlsettlement.Clone(settlement)
	if settlement.Phase == 0 {
		settlement.Phase = crawlsettlement.AwaitingAcknowledgment
	}
	if settlement.Phase != crawlsettlement.AwaitingAcknowledgment {
		return crawlsettlement.ErrDefinitionConflict
	}
	if !validTerminalSettlementKey(settlement) {
		return crawlsettlement.ErrDefinitionConflict
	}
	return checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		return stageTerminalSettlement(transaction, settlement)
	})
}

func stageTerminalSettlement(
	transaction *bolt.Tx,
	settlement crawlsettlement.Settlement,
) error {
	outbox, err := schemaBucket(transaction, terminalOutboxBucket)
	if err != nil {
		return err
	}
	encoded := outbox.Get([]byte(settlement.LeaseID))
	if encoded != nil {
		return stageExistingTerminalSettlement(outbox, encoded, settlement)
	}
	record, found, err := readRunRecord(transaction, settlement.Provenance)
	if err != nil {
		return err
	}
	if !found || record.Deleting || !record.Completed ||
		!bytes.Equal(record.OrderIdentity, settlement.OrderIdentity) {
		return crawlsettlement.ErrDefinitionConflict
	}
	encoded, encodeErr := encodeRow("terminal settlement", settlement)
	return errors.Join(
		encodeErr,
		putRow(outbox, []byte(settlement.LeaseID), encoded, "terminal settlement"),
	)
}

func stageExistingTerminalSettlement(
	outbox *bolt.Bucket,
	encoded []byte,
	settlement crawlsettlement.Settlement,
) error {
	persisted, err := decodeTerminalSettlement(encoded)
	if err != nil {
		return err
	}
	if crawlsettlement.SameDefinition(persisted, settlement) {
		return nil
	}
	persisted, err = rebindAwaitingTerminalSettlement(
		persisted,
		settlement,
		settlement.WorkerSessionID,
	)
	if err != nil {
		return err
	}

	return writeTerminalSettlement(outbox, persisted)
}

func (checkpoint *FrontierCheckpoint) Awaiting(
	ctx context.Context,
) ([]crawlsettlement.Settlement, error) {
	checkpoint.terminalSettlementReconciliationMutex.Lock()
	defer checkpoint.terminalSettlementReconciliationMutex.Unlock()
	awaiting := []crawlsettlement.Settlement(nil)
	var lastKey []byte
	err := checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		outbox, err := schemaBucket(transaction, terminalOutboxBucket)
		if err != nil {
			return err
		}
		var readErr error
		awaiting, lastKey, readErr = readAwaitingTerminalSettlements(
			outbox,
			checkpoint.terminalSettlementReconciliationCursor,
		)

		return readErr
	})
	if err == nil && lastKey != nil {
		checkpoint.terminalSettlementReconciliationCursor = lastKey
	}

	return awaiting, err
}

func readAwaitingTerminalSettlements(
	outbox *bolt.Bucket,
	previous []byte,
) ([]crawlsettlement.Settlement, []byte, error) {
	cursor := outbox.Cursor()
	key, encoded := terminalSettlementCursorStart(cursor, previous)
	awaiting := make([]crawlsettlement.Settlement, 0, terminalSettlementReconciliationBatchSize)
	var firstKey []byte
	var lastKey []byte
	for key != nil && len(awaiting) < terminalSettlementReconciliationBatchSize {
		if firstKey == nil {
			firstKey = bytes.Clone(key)
		} else if bytes.Equal(key, firstKey) {
			break
		}
		settlement, err := decodeTerminalSettlement(encoded)
		if err != nil {
			return nil, nil, err
		}
		if !bytes.Equal(key, []byte(settlement.LeaseID)) {
			return nil, nil, fmt.Errorf(
				"%w: terminal settlement key mismatch",
				ErrCorruptCheckpoint,
			)
		}
		awaiting = append(awaiting, settlement)
		lastKey = bytes.Clone(key)
		key, encoded = nextTerminalSettlement(cursor)
	}

	return awaiting, lastKey, nil
}

func terminalSettlementCursorStart(
	cursor *bolt.Cursor,
	previous []byte,
) ([]byte, []byte) {
	if len(previous) == 0 {
		return cursor.First()
	}
	key, encoded := cursor.Seek(previous)
	if bytes.Equal(key, previous) {
		key, encoded = cursor.Next()
	}
	if key == nil {
		return cursor.First()
	}

	return key, encoded
}

func nextTerminalSettlement(cursor *bolt.Cursor) ([]byte, []byte) {
	key, encoded := cursor.Next()
	if key == nil {
		return cursor.First()
	}

	return key, encoded
}

func (checkpoint *FrontierCheckpoint) Current(
	ctx context.Context,
	leaseID string,
	orderIdentity []byte,
) (crawlsettlement.Settlement, bool, error) {
	return checkpoint.terminalSettlement(ctx, leaseID, orderIdentity)
}

func (checkpoint *FrontierCheckpoint) RecordAcknowledgment(
	ctx context.Context,
	leaseID string,
	orderIdentity []byte,
	confirmationToken []byte,
) error {
	if leaseID == "" || len(leaseID) > bolt.MaxKeySize || len(orderIdentity) == 0 ||
		len(confirmationToken) != 32 {
		return crawlsettlement.ErrDefinitionConflict
	}
	return checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		outbox, settlement, found, err := readTerminalSettlement(
			transaction,
			leaseID,
			orderIdentity,
		)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if settlement.Phase != crawlsettlement.AwaitingAcknowledgment {
			if !bytes.Equal(settlement.ConfirmationToken, confirmationToken) {
				return crawlsettlement.ErrDefinitionConflict
			}

			return nil
		}
		settlement.Phase = crawlsettlement.AcknowledgedDeleting
		settlement.ConfirmationToken = append([]byte(nil), confirmationToken...)

		return writeTerminalSettlement(outbox, settlement)
	})
}

func (checkpoint *FrontierCheckpoint) PrepareConfirmation(
	ctx context.Context,
	leaseID string,
	orderIdentity []byte,
) error {
	settlement, found, err := checkpoint.terminalSettlement(ctx, leaseID, orderIdentity)
	if err != nil || !found || settlement.Phase == crawlsettlement.Confirming {
		return err
	}
	if settlement.Phase != crawlsettlement.AcknowledgedDeleting {
		return crawlsettlement.ErrDefinitionConflict
	}
	if err := checkpoint.Delete(ctx, settlement.Provenance); err != nil {
		return err
	}
	return checkpoint.writeTransaction(
		ctx,
		advanceTerminalConfirmation(leaseID, orderIdentity, settlement.ConfirmationToken),
	)
}

func (checkpoint *FrontierCheckpoint) Complete(
	ctx context.Context,
	leaseID string,
	orderIdentity []byte,
) error {
	return checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		outbox, settlement, found, err := readTerminalSettlement(
			transaction,
			leaseID,
			orderIdentity,
		)
		if err != nil || !found {
			return err
		}
		if settlement.Phase != crawlsettlement.Confirming {
			return crawlsettlement.ErrDefinitionConflict
		}

		return deleteRow(outbox, []byte(leaseID), "terminal settlement")
	})
}

func (checkpoint *FrontierCheckpoint) terminalSettlement(
	ctx context.Context,
	leaseID string,
	orderIdentity []byte,
) (crawlsettlement.Settlement, bool, error) {
	var settlement crawlsettlement.Settlement
	found := false
	err := checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		_, current, exists, err := readTerminalSettlement(transaction, leaseID, orderIdentity)
		if err != nil || !exists {
			return err
		}
		settlement = current
		found = true

		return nil
	})

	return settlement, found, err
}

func readTerminalSettlement(
	transaction *bolt.Tx,
	leaseID string,
	orderIdentity []byte,
) (*bolt.Bucket, crawlsettlement.Settlement, bool, error) {
	if leaseID == "" || len(leaseID) > bolt.MaxKeySize || len(orderIdentity) == 0 {
		return nil, crawlsettlement.Settlement{}, false, crawlsettlement.ErrDefinitionConflict
	}
	outbox, err := schemaBucket(transaction, terminalOutboxBucket)
	if err != nil {
		return nil, crawlsettlement.Settlement{}, false, err
	}
	encoded := outbox.Get([]byte(leaseID))
	if encoded == nil {
		return outbox, crawlsettlement.Settlement{}, false, nil
	}
	settlement, err := decodeTerminalSettlement(encoded)
	if err != nil {
		return nil, crawlsettlement.Settlement{}, false, err
	}
	if settlement.LeaseID != leaseID || !bytes.Equal(settlement.OrderIdentity, orderIdentity) {
		return nil, crawlsettlement.Settlement{}, false, crawlsettlement.ErrDefinitionConflict
	}

	return outbox, settlement, true, nil
}

func writeTerminalSettlement(
	outbox *bolt.Bucket,
	settlement crawlsettlement.Settlement,
) error {
	encoded, err := encodeRow("terminal settlement", settlement)
	return errors.Join(
		err,
		putRow(outbox, []byte(settlement.LeaseID), encoded, "terminal settlement"),
	)
}

func validTerminalSettlementKey(settlement crawlsettlement.Settlement) bool {
	if !crawlsettlement.Validate(settlement) || len(settlement.LeaseID) > bolt.MaxKeySize {
		return false
	}
	_, err := provenancePrefix(settlement.Provenance)

	return err == nil
}

func decodeTerminalSettlement(encoded []byte) (crawlsettlement.Settlement, error) {
	var settlement crawlsettlement.Settlement
	if err := decodeRow("terminal settlement", encoded, &settlement); err != nil {
		return crawlsettlement.Settlement{}, err
	}
	if !validTerminalSettlementKey(settlement) {
		return crawlsettlement.Settlement{}, fmt.Errorf(
			"%w: invalid terminal settlement",
			ErrCorruptCheckpoint,
		)
	}

	return crawlsettlement.Clone(settlement), nil
}
