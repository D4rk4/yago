package crawlbroker

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const terminalSettlementSecretBucket vault.Name = "crawlordersettlementsecret"

var (
	terminalSettlementSecretKey           = vault.Key("hmac")
	terminalSettlementEntropy   io.Reader = rand.Reader
)

func loadTerminalSettlementSecret(
	storage *vault.Vault,
	secrets *vault.Collection[[]byte],
) ([]byte, error) {
	var secret []byte
	err := storage.Update(context.Background(), func(transaction *vault.Txn) error {
		persisted, found, _ := secrets.Get(transaction, terminalSettlementSecretKey)
		if found {
			if len(persisted) != sha256.Size {
				return fmt.Errorf("read terminal settlement secret: invalid length")
			}
			secret = append([]byte(nil), persisted...)

			return nil
		}
		secret = make([]byte, sha256.Size)
		if _, err := io.ReadFull(terminalSettlementEntropy, secret); err != nil {
			return fmt.Errorf("create terminal settlement secret: %w", err)
		}
		if err := secrets.Put(transaction, terminalSettlementSecretKey, secret); err != nil {
			return fmt.Errorf("store terminal settlement secret: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load terminal settlement secret: %w", err)
	}

	return secret, nil
}

func terminalSettlementToken(
	secret []byte,
	leaseID string,
	request terminalLeaseRequest,
) []byte {
	digest := hmac.New(sha256.New, secret)
	writeTerminalTokenBytes(digest, []byte(leaseID))
	digest.Write([]byte{byte(request.Outcome)})
	writeTerminalTokenBytes(digest, request.OrderIdentity)
	writeTerminalTokenBytes(digest, []byte(request.WorkerID))
	writeTerminalTokenBytes(digest, []byte(request.WorkerSessionID))
	writeTerminalTokenBytes(digest, []byte(request.State))
	values := [...]uint64{
		request.Tally.Fetched,
		request.Tally.Indexed,
		request.Tally.Failed,
		request.Tally.RobotsDenied,
		request.Tally.Duplicates,
		request.Tally.Pending,
		uint64(request.Rate),
	}
	var encoded [8]byte
	for _, value := range values {
		binary.BigEndian.PutUint64(encoded[:], value)
		digest.Write(encoded[:])
	}
	if request.RateKnown {
		digest.Write([]byte{1})
	} else {
		digest.Write([]byte{0})
	}

	return digest.Sum(nil)
}

func writeTerminalTokenBytes(destination io.Writer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

func validTerminalSettlementToken(
	secret []byte,
	leaseID string,
	request terminalLeaseRequest,
) bool {
	if len(request.ConfirmationToken) != sha256.Size {
		return false
	}
	want := terminalSettlementToken(secret, leaseID, request)

	return hmac.Equal(want, request.ConfirmationToken)
}
