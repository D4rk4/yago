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
	seed       yagomodel.Seed
	lastSeen   time.Time
	retryAfter time.Time
	expiresAt  time.Time
	verified   bool
}

type rosterEntryCodec struct{}

func (rosterEntryCodec) Encode(entry rosterEntry) ([]byte, error) {
	encoded := entry.seed.String()
	if _, err := yagomodel.ParseSeed(context.Background(), encoded); err != nil {
		return nil, fmt.Errorf("encode roster entry: %w", err)
	}

	return encodeLegacyRosterEntry(entry.lastSeen, encoded), nil
}

func encodeLegacyRosterEntry(lastSeen time.Time, encodedSeed string) []byte {
	out := make([]byte, lastSeenWidth, lastSeenWidth+len(encodedSeed))
	binary.BigEndian.PutUint64(out, uint64(lastSeen.UnixNano()))

	return append(out, encodedSeed...)
}

func (rosterEntryCodec) Decode(raw []byte) (rosterEntry, error) {
	return decodeLegacyRosterEntry(raw)
}

func decodeLegacyRosterEntry(raw []byte) (rosterEntry, error) {
	if len(raw) < lastSeenWidth {
		return rosterEntry{}, fmt.Errorf("decode roster entry: short record")
	}

	nanos := signedBigEndianInt64(raw[:lastSeenWidth])
	seed, err := yagomodel.ParseSeed(context.Background(), string(raw[lastSeenWidth:]))
	if err != nil {
		return rosterEntry{}, fmt.Errorf("decode roster entry: %w", err)
	}

	lastSeen := time.Unix(0, nanos)

	return rosterEntry{
		seed:      seed,
		lastSeen:  lastSeen,
		expiresAt: lastSeen.Add(peerPassiveRetention),
	}, nil
}

func rosterEntryUnixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}

	return value.UnixNano()
}

func rosterEntryTime(raw []byte) time.Time {
	nanos := signedBigEndianInt64(raw)
	if nanos == 0 {
		return time.Time{}
	}

	return time.Unix(0, nanos)
}

func signedBigEndianInt64(raw []byte) int64 {
	return int64(binary.BigEndian.Uint32(raw[:4]))<<32 |
		int64(binary.BigEndian.Uint32(raw[4:]))
}
