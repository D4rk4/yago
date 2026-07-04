package yagomodel

import (
	"errors"
	"fmt"
)

var errInvalidBitfield = errors.New("invalid bitfield")

type Bitfield []byte

func DecodeBitfield(encoded string) (Bitfield, error) {
	raw, err := Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidBitfield, err)
	}
	return Bitfield(raw), nil
}

func (b Bitfield) Get(pos int) bool {
	slot := pos >> 3
	if pos < 0 || slot >= len(b) {
		return false
	}
	return b[slot]&(1<<(pos%8)) != 0
}

func (b Bitfield) AllSet(bits int) bool {
	for pos := range bits {
		if !b.Get(pos) {
			return false
		}
	}
	return true
}
