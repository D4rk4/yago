package peerroster

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

const lastSeenWidth = 8

type rosterEntry struct {
	seed     yagomodel.Seed
	lastSeen time.Time
}

type rosterEntryCodec struct{}

func (rosterEntryCodec) Encode(entry rosterEntry) ([]byte, error) {
	encoded := entry.seed.String()
	if _, err := yagomodel.ParseSeed(context.Background(), encoded); err != nil {
		return nil, fmt.Errorf("encode roster entry: %w", err)
	}
	out := make([]byte, lastSeenWidth, lastSeenWidth+len(encoded))
	binary.BigEndian.PutUint64(out, uint64(entry.lastSeen.UnixNano()))

	return append(out, encoded...), nil
}

func (rosterEntryCodec) Decode(raw []byte) (rosterEntry, error) {
	if len(raw) < lastSeenWidth {
		return rosterEntry{}, fmt.Errorf("decode roster entry: short record")
	}

	//nolint:gosec // round-trips an int64 UnixNano stored as fixed-width bytes
	nanos := int64(binary.BigEndian.Uint64(raw[:lastSeenWidth]))
	seed, err := yagomodel.ParseSeed(context.Background(), string(raw[lastSeenWidth:]))
	if err != nil {
		return rosterEntry{}, fmt.Errorf("decode roster entry: %w", err)
	}

	return rosterEntry{seed: seed, lastSeen: time.Unix(0, nanos)}, nil
}
