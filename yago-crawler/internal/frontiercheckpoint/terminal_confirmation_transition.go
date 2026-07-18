package frontiercheckpoint

import (
	"bytes"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
)

func advanceTerminalConfirmation(
	leaseID string,
	orderIdentity []byte,
	expectedToken []byte,
) func(*bolt.Tx) error {
	return func(transaction *bolt.Tx) error {
		outbox, current, found, err := readTerminalSettlement(
			transaction,
			leaseID,
			orderIdentity,
		)
		if err != nil || !found {
			return err
		}
		if !bytes.Equal(current.ConfirmationToken, expectedToken) {
			return crawlsettlement.ErrDefinitionConflict
		}
		if current.Phase == crawlsettlement.Confirming {
			return nil
		}
		if current.Phase != crawlsettlement.AcknowledgedDeleting {
			return crawlsettlement.ErrDefinitionConflict
		}
		current.Phase = crawlsettlement.Confirming

		return writeTerminalSettlement(outbox, current)
	}
}
