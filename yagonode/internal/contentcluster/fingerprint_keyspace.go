package contentcluster

import (
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type fingerprintKeyspace struct {
	entries *vault.Keyspace[json.RawMessage]
}

func registerFingerprintKeyspace(v *vault.Vault) (*fingerprintKeyspace, error) {
	entries, err := vault.RegisterKeyspace(
		v,
		fingerprintBucketName,
		jsonCodec[json.RawMessage]{},
	)
	if err != nil {
		return nil, fmt.Errorf("register fingerprint keyspace: %w", err)
	}

	return &fingerprintKeyspace{entries: entries}, nil
}

func (k *fingerprintKeyspace) Get(
	tx *vault.Txn,
	key vault.Key,
) (fingerprintRecord, bool, error) {
	return readFingerprintRecord(k.entries, tx, key)
}

func (k *fingerprintKeyspace) Put(
	tx *vault.Txn,
	key vault.Key,
	record fingerprintRecord,
) error {
	return putFingerprintRecord(k.entries, tx, key, record)
}

func (k *fingerprintKeyspace) Delete(tx *vault.Txn, key vault.Key) (bool, error) {
	deleted, err := k.entries.Delete(tx, key)
	if err != nil {
		return false, fmt.Errorf("delete fingerprint entry: %w", err)
	}

	return deleted, nil
}

func (k *fingerprintKeyspace) transition(
	tx *vault.Txn,
	url string,
) (fingerprintTransition, bool, error) {
	raw, found, err := k.entries.Get(tx, transitionKey(url))
	if err != nil || !found {
		if err != nil {
			return fingerprintTransition{}, false, fmt.Errorf(
				"read fingerprint transition: %w",
				err,
			)
		}

		return fingerprintTransition{}, false, nil
	}
	var transition fingerprintTransition
	if err := json.Unmarshal(raw, &transition); err != nil {
		return fingerprintTransition{}, false, fmt.Errorf("decode fingerprint transition: %w", err)
	}
	if transition.URL != url || transition.Token == "" {
		return fingerprintTransition{}, false, fmt.Errorf(
			"fingerprint transition identity is invalid",
		)
	}

	return transition, true, nil
}

func (k *fingerprintKeyspace) putTransition(
	tx *vault.Txn,
	transition fingerprintTransition,
) error {
	raw, err := json.Marshal(transition)
	if err != nil {
		return fmt.Errorf("encode fingerprint transition: %w", err)
	}

	if err := k.entries.Put(tx, transitionKey(transition.URL), raw); err != nil {
		return fmt.Errorf("store fingerprint transition entry: %w", err)
	}

	return nil
}

func (k *fingerprintKeyspace) deleteTransition(
	tx *vault.Txn,
	finalization EvidenceFinalization,
) (bool, error) {
	transition, found, err := k.transition(tx, finalization.url)
	if err != nil || !found || transition.Token != finalization.token {
		return false, err
	}

	deleted, err := k.entries.Delete(tx, transitionKey(finalization.url))
	if err != nil {
		return false, fmt.Errorf("delete fingerprint transition entry: %w", err)
	}

	return deleted, nil
}

func readFingerprintRecord(
	entries *vault.Keyspace[json.RawMessage],
	tx *vault.Txn,
	key vault.Key,
) (fingerprintRecord, bool, error) {
	raw, found, err := entries.Get(tx, key)
	if err != nil || !found {
		if err != nil {
			return fingerprintRecord{}, false, fmt.Errorf("read fingerprint entry: %w", err)
		}

		return fingerprintRecord{}, false, nil
	}
	var record fingerprintRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return fingerprintRecord{}, false, fmt.Errorf("decode content fingerprint: %w", err)
	}

	return record, true, nil
}

func putFingerprintRecord(
	entries *vault.Keyspace[json.RawMessage],
	tx *vault.Txn,
	key vault.Key,
	record fingerprintRecord,
) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode content fingerprint: %w", err)
	}

	if err := entries.Put(tx, key, raw); err != nil {
		return fmt.Errorf("store fingerprint entry: %w", err)
	}

	return nil
}

func transitionKey(url string) vault.Key {
	key := make(vault.Key, len(url)+1)
	copy(key[1:], url)

	return key
}
