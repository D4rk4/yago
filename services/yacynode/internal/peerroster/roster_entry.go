package peerroster

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const lastSeenWidth = 8

type rosterEntry struct {
	seed     yacymodel.Seed
	lastSeen time.Time
}

type rosterEntryCodec struct{}

func (rosterEntryCodec) Encode(entry rosterEntry) ([]byte, error) {
	out := make([]byte, lastSeenWidth, lastSeenWidth+len(entry.seed.String()))
	binary.BigEndian.PutUint64(out, uint64(entry.lastSeen.UnixNano()))

	return append(out, entry.seed.String()...), nil
}

func (rosterEntryCodec) Decode(raw []byte) (rosterEntry, error) {
	if len(raw) < lastSeenWidth {
		return rosterEntry{}, fmt.Errorf("decode roster entry: short record")
	}

	//nolint:gosec // round-trips an int64 UnixNano stored as fixed-width bytes
	nanos := int64(binary.BigEndian.Uint64(raw[:lastSeenWidth]))
	seed, err := yacymodel.ParseSeed(context.Background(), string(raw[lastSeenWidth:]))
	if err != nil {
		return rosterEntry{}, fmt.Errorf("decode roster entry: %w", err)
	}

	return rosterEntry{seed: seed, lastSeen: time.Unix(0, nanos)}, nil
}
