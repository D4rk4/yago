// Package peeridentity keeps this peer's stable identity — its hash and name —
// so a node bootstraps without the operator having to supply them. When neither
// the environment nor a previous run provided a value, one is generated and
// persisted to the data directory, and reused on every later start so the peer
// keeps the same identity (and its standing on the network) across restarts.
package peeridentity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const identityBucket vault.Name = "peeridentity"

var identityKey = vault.Key("identity")

const (
	peerNamePrefix       = "yago-"
	peerNameEntropyBytes = 4
	recordSeparator      = "\n"
	recordFields         = 2
)

// record is the persisted identity: a peer hash and name stored together so a
// single read validates the whole identity.
type record struct {
	hash yagomodel.Hash
	name string
}

type recordCodec struct{}

func (recordCodec) Encode(r record) ([]byte, error) {
	return []byte(string(r.hash) + recordSeparator + r.name), nil
}

func (recordCodec) Decode(raw []byte) (record, error) {
	parts := strings.SplitN(string(raw), recordSeparator, recordFields)
	if len(parts) != recordFields {
		return record{}, fmt.Errorf("malformed peer identity record %q", raw)
	}
	hash, err := yagomodel.ParseHash(parts[0])
	if err != nil {
		return record{}, fmt.Errorf("parse stored peer hash: %w", err)
	}

	return record{hash: hash, name: parts[1]}, nil
}

// Generators produce a fresh identity when none is supplied or stored. They are
// injected so tests can make generation deterministic.
type Generators struct {
	Hash func() (yagomodel.Hash, error)
	Name func() (string, error)
}

// DefaultGenerators generate a random peer hash and a "yago-"-prefixed name.
func DefaultGenerators() Generators {
	return Generators{Hash: yagomodel.NewHash, Name: NewName}
}

// NewName generates a random default peer name from the system CSPRNG.
func NewName() (string, error) {
	return GenerateName(rand.Reader)
}

// GenerateName returns a random, human-readable default peer name.
func GenerateName(entropy io.Reader) (string, error) {
	buf := make([]byte, peerNameEntropyBytes)
	if _, err := io.ReadFull(entropy, buf); err != nil {
		return "", fmt.Errorf("read entropy: %w", err)
	}

	return peerNamePrefix + hex.EncodeToString(buf), nil
}

// Open resolves the effective peer hash and name. An explicit value (declared,
// from the environment) is authoritative and is also recorded so the identity
// stays stable if the operator later unsets it. Otherwise a previously stored
// value is reused, or a new one is generated. The effective identity is always
// persisted to the data directory.
func Open(
	ctx context.Context,
	v *vault.Vault,
	declaredHash yagomodel.Hash,
	declaredName string,
	gen Generators,
) (yagomodel.Hash, string, error) {
	store, err := vault.Register(v, identityBucket, recordCodec{})
	if err != nil {
		return "", "", fmt.Errorf("register peer identity: %w", err)
	}

	var resolved record
	err = v.Update(ctx, func(tx *vault.Txn) error {
		stored, ok, err := store.Get(tx, identityKey)
		if err != nil {
			return fmt.Errorf("read stored peer identity: %w", err)
		}
		resolved, err = effectiveIdentity(declaredHash, declaredName, stored, ok, gen)
		if err != nil {
			return err
		}

		return store.Put(tx, identityKey, resolved)
	})
	if err != nil {
		return "", "", fmt.Errorf("load peer identity: %w", err)
	}

	return resolved.hash, resolved.name, nil
}

func effectiveIdentity(
	declaredHash yagomodel.Hash,
	declaredName string,
	stored record,
	ok bool,
	gen Generators,
) (record, error) {
	hash, err := effectiveHash(declaredHash, stored, ok, gen.Hash)
	if err != nil {
		return record{}, err
	}
	name, err := effectiveName(declaredName, stored, ok, gen.Name)
	if err != nil {
		return record{}, err
	}

	return record{hash: hash, name: name}, nil
}

func effectiveHash(
	declared yagomodel.Hash,
	stored record,
	ok bool,
	generate func() (yagomodel.Hash, error),
) (yagomodel.Hash, error) {
	if declared != "" {
		return declared, nil
	}
	if ok {
		return stored.hash, nil
	}
	hash, err := generate()
	if err != nil {
		return "", fmt.Errorf("generate peer hash: %w", err)
	}

	return hash, nil
}

func effectiveName(
	declared string,
	stored record,
	ok bool,
	generate func() (string, error),
) (string, error) {
	if declared != "" {
		return declared, nil
	}
	if ok {
		return stored.name, nil
	}
	name, err := generate()
	if err != nil {
		return "", fmt.Errorf("generate peer name: %w", err)
	}

	return name, nil
}
