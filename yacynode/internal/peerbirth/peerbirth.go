// Package peerbirth keeps this peer's birth date: the moment the node first
// started with its data directory. The birth date survives restarts so remote
// peers judge this peer's age from its real history, not from the latest
// process start.
package peerbirth

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

const birthBucket vault.Name = "peerbirth"

var birthKey = vault.Key("birthdate")

type birthCodec struct{}

func (birthCodec) Encode(t time.Time) ([]byte, error) {
	return t.UTC().AppendFormat(nil, time.RFC3339), nil
}

func (birthCodec) Decode(raw []byte) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, string(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("decode peer birth date: %w", err)
	}

	return t, nil
}

func Open(
	ctx context.Context,
	v *vault.Vault,
	now func() time.Time,
	declared time.Time,
) (time.Time, error) {
	births, err := vault.Register(v, birthBucket, birthCodec{})
	if err != nil {
		return time.Time{}, fmt.Errorf("register peer birth date: %w", err)
	}

	var birth time.Time
	err = v.Update(ctx, func(tx *vault.Txn) error {
		stored, ok, err := births.Get(tx, birthKey)
		if err != nil {
			return fmt.Errorf("read stored peer birth date: %w", err)
		}
		if ok {
			birth = stored

			return nil
		}
		birth = now().UTC().Truncate(time.Second)
		if !declared.IsZero() {
			birth = declared.UTC().Truncate(time.Second)
		}

		return births.Put(tx, birthKey, birth)
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("load peer birth date: %w", err)
	}

	return birth, nil
}
