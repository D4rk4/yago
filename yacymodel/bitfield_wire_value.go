package yacymodel

import (
	"errors"
	"fmt"
)

var errInvalidBitfield = errors.New("invalid bitfield")

func validateEncodedBitfieldMaxBytes(s string, byteLength int) error {
	raw, err := Decode(s)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidBitfield, err)
	}
	if len(raw) > byteLength {
		return fmt.Errorf(
			"%w: length %d, want at most %d",
			errInvalidBitfield,
			len(raw),
			byteLength,
		)
	}
	return nil
}
