package peerroster

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const rosterLifecycleCleanupCursorMaximumWidth = 32 << 10

var rosterLifecycleCleanupCursorKey = vault.Key("position")

type rosterLifecycleCleanupCursorCodec struct{}

func (rosterLifecycleCleanupCursorCodec) Encode(cursor vault.Key) ([]byte, error) {
	if len(cursor) == 0 || len(cursor) > rosterLifecycleCleanupCursorMaximumWidth {
		return nil, fmt.Errorf("encode roster lifecycle cleanup cursor: invalid width")
	}

	return append([]byte(nil), cursor...), nil
}

func (rosterLifecycleCleanupCursorCodec) Decode(raw []byte) (vault.Key, error) {
	if len(raw) == 0 || len(raw) > rosterLifecycleCleanupCursorMaximumWidth {
		return nil, fmt.Errorf("decode roster lifecycle cleanup cursor: invalid width")
	}

	return append(vault.Key(nil), raw...), nil
}
