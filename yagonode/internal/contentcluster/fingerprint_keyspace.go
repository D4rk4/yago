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
		rawJSONCodec{},
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
	return readTransitionShape(k.entries, tx, url, func(t fingerprintTransition) (string, string) {
		return t.URL, t.Token
	})
}

// match reads only the fields posting visibility compares. See fingerprintMatch
// for why the shingle vector is not built.
func (k *fingerprintKeyspace) match(
	tx *vault.Txn,
	key vault.Key,
) (fingerprintMatch, bool, error) {
	return readFingerprintShape[fingerprintMatch](k.entries, tx, key)
}

func (k *fingerprintKeyspace) transitionMatch(
	tx *vault.Txn,
	url string,
) (fingerprintTransitionMatch, bool, error) {
	return readTransitionShape(
		k.entries,
		tx,
		url,
		func(t fingerprintTransitionMatch) (string, string) { return t.URL, t.Token },
	)
}

// readTransitionShape reads a transition entry into either the full shape or
// the reduced one. The two differ only in how much of each embedded fingerprint
// they build, so the read, the decode failure and the identity check have to
// stay identical between them -- sharing this function is what guarantees that,
// rather than a comment asking two copies to be kept in step.
func readTransitionShape[Shape any](
	entries *vault.Keyspace[json.RawMessage],
	tx *vault.Txn,
	url string,
	identity func(Shape) (string, string),
) (Shape, bool, error) {
	var zero Shape
	raw, found, err := entries.Get(tx, transitionKey(url))
	if err != nil || !found {
		if err != nil {
			return zero, false, fmt.Errorf("read fingerprint transition: %w", err)
		}

		return zero, false, nil
	}
	var shape Shape
	if err := json.Unmarshal(raw, &shape); err != nil {
		return zero, false, fmt.Errorf("decode fingerprint transition: %w", err)
	}
	storedURL, token := identity(shape)
	if storedURL != url || token == "" {
		return zero, false, fmt.Errorf("fingerprint transition identity is invalid")
	}

	return shape, true, nil
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
	return readFingerprintShape[fingerprintRecord](entries, tx, key)
}

// readFingerprintShape reads a stored fingerprint into either the full record
// or the reduced match shape, so both see the same absence and the same decode
// failure.
func readFingerprintShape[Shape any](
	entries *vault.Keyspace[json.RawMessage],
	tx *vault.Txn,
	key vault.Key,
) (Shape, bool, error) {
	var zero Shape
	raw, found, err := entries.Get(tx, key)
	if err != nil || !found {
		if err != nil {
			return zero, false, fmt.Errorf("read fingerprint entry: %w", err)
		}

		return zero, false, nil
	}
	var shape Shape
	if err := json.Unmarshal(raw, &shape); err != nil {
		return zero, false, fmt.Errorf("decode content fingerprint: %w", err)
	}

	return shape, true, nil
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
