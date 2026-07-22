package peerroster

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

const (
	rosterLifecycleMetadataWidth = 8 + 8 + 1
	rosterLifecycleEnvelopeWidth = 4 + sha256.Size + rosterLifecycleMetadataWidth
)

var rosterLifecycleMagic = []byte{'Y', 'P', 'L', '1'}

type rosterLifecycle struct {
	rowIdentity [sha256.Size]byte
	retryAfter  time.Time
	expiresAt   time.Time
	verified    bool
}

type rosterLifecycleCodec struct{}

func (rosterLifecycleCodec) Encode(lifecycle rosterLifecycle) ([]byte, error) {
	raw := make([]byte, rosterLifecycleEnvelopeWidth)
	copy(raw[:4], rosterLifecycleMagic)
	copy(raw[4:4+sha256.Size], lifecycle.rowIdentity[:])
	metadata := encodeRosterLifecycleMetadata(lifecycle)
	copy(raw[4+sha256.Size:], metadata[:])

	return raw, nil
}

func (rosterLifecycleCodec) Decode(raw []byte) (rosterLifecycle, error) {
	if len(raw) != rosterLifecycleEnvelopeWidth {
		return rosterLifecycle{}, fmt.Errorf("decode roster lifecycle: invalid record width")
	}
	if !bytes.Equal(raw[:4], rosterLifecycleMagic) {
		return rosterLifecycle{}, fmt.Errorf("decode roster lifecycle: invalid record identity")
	}
	if raw[20+sha256.Size] > 1 {
		return rosterLifecycle{}, fmt.Errorf("decode roster lifecycle: invalid verification state")
	}
	var rowIdentity [sha256.Size]byte
	copy(rowIdentity[:], raw[4:4+sha256.Size])

	return rosterLifecycle{
		rowIdentity: rowIdentity,
		retryAfter:  rosterEntryTime(raw[4+sha256.Size : 12+sha256.Size]),
		expiresAt:   rosterEntryTime(raw[12+sha256.Size : 20+sha256.Size]),
		verified:    raw[20+sha256.Size] == 1,
	}, nil
}

func rosterLifecycleFor(entry rosterEntry) (rosterLifecycle, error) {
	raw, err := (rosterEntryCodec{}).Encode(entry)
	if err != nil {
		return rosterLifecycle{}, err
	}
	lifecycle := rosterLifecycle{
		retryAfter: entry.retryAfter,
		expiresAt:  entry.expiresAt,
		verified:   entry.verified,
	}
	lifecycle.rowIdentity = rosterLifecycleIdentity(raw, lifecycle)

	return lifecycle, nil
}

func (lifecycle rosterLifecycle) appliesTo(entry rosterEntry) bool {
	encoded := encodeLegacyRosterEntry(entry.lastSeen, entry.seed.String())

	return lifecycle.rowIdentity == rosterLifecycleIdentity(encoded, lifecycle)
}

func rosterLifecycleIdentity(raw []byte, lifecycle rosterLifecycle) [sha256.Size]byte {
	metadata := encodeRosterLifecycleMetadata(lifecycle)
	bound := make([]byte, 0, len(raw)+len(metadata))
	bound = append(bound, raw...)
	bound = append(bound, metadata[:]...)

	return sha256.Sum256(bound)
}

func encodeRosterLifecycleMetadata(lifecycle rosterLifecycle) [rosterLifecycleMetadataWidth]byte {
	var metadata [rosterLifecycleMetadataWidth]byte
	binary.BigEndian.PutUint64(
		metadata[:8],
		uint64(rosterEntryUnixNano(lifecycle.retryAfter)),
	)
	binary.BigEndian.PutUint64(
		metadata[8:16],
		uint64(rosterEntryUnixNano(lifecycle.expiresAt)),
	)
	if lifecycle.verified {
		metadata[16] = 1
	}

	return metadata
}
