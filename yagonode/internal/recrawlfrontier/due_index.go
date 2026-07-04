package recrawlfrontier

import (
	"bytes"
	"fmt"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const dueKeySeparator = 0x00

// dueKey orders a URL's recrawl entry by its next-due time: a fixed-width,
// zero-padded UnixNano prefix sorts lexicographically in chronological order, so
// a forward scan of the due index yields the soonest-due URLs first. The URL hash
// after the separator disambiguates entries that fall due at the same instant.
func dueKey(nextDueAt time.Time, hash string) vault.Key {
	nanos := nextDueAt.UnixNano()
	if nanos < 0 {
		nanos = 0
	}
	var key bytes.Buffer
	fmt.Fprintf(&key, "%019d", nanos)
	key.WriteByte(dueKeySeparator)
	key.WriteString(hash)

	return key.Bytes()
}

func nextDueFromKey(key vault.Key) (time.Time, error) {
	index := bytes.IndexByte(key, dueKeySeparator)
	if index < 0 {
		return time.Time{}, fmt.Errorf("recrawl due key without separator")
	}
	nanos, err := strconv.ParseInt(string(key[:index]), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("recrawl due key time: %w", err)
	}

	return time.Unix(0, nanos).UTC(), nil
}

func hashFromDueKey(key vault.Key) (string, error) {
	index := bytes.IndexByte(key, dueKeySeparator)
	if index < 0 {
		return "", fmt.Errorf("recrawl due key without separator")
	}

	return string(key[index+1:]), nil
}

type presenceCodec struct{}

func (presenceCodec) Encode(struct{}) ([]byte, error) { return []byte{}, nil }

func (presenceCodec) Decode([]byte) (struct{}, error) { return struct{}{}, nil }
