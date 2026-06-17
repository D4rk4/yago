package yacymodel

import (
	"errors"
	"fmt"
	"strconv"
)

const (
	rwiByteFlagLength = 4
	langLength        = 2
)

var errInvalidRWIProperty = errors.New("invalid rwi property")

var rwiCardinalWidths = map[string]int{
	ColLastModified:      2,
	ColFreshUntil:        2,
	ColTitleWordCount:    1,
	ColTextWordCount:     2,
	ColPhraseCount:       2,
	ColLocalLinkCount:    1,
	ColExternalLinkCount: 1,
	ColURLLength:         1,
	ColURLComponentCount: 1,
	ColHitCount:          1,
	ColTextPosition:      2,
	ColPhraseRelativePos: 1,
	ColPhrasePosition:    1,
	ColWordDistance:      1,
	ColReserve:           1,
}

var rwiBinaryWidths = map[string]int{
	ColDocType:  1,
	ColWordType: 1,
}

func validateRWIProperties(props map[string]string) error {
	for key, value := range props {
		if err := validateRWIProperty(key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateRWIProperty(key, value string) error {
	switch key {
	case ColURLHash:
		if _, err := ParseHash(value); err != nil {
			return fmt.Errorf("%w %s: %w", errInvalidRWIProperty, key, err)
		}
	case ColLanguage:
		if len(value) != langLength {
			return fmt.Errorf(
				"%w %s: length %d, want %d",
				errInvalidRWIProperty,
				key,
				len(value),
				langLength,
			)
		}
	case ColFlags:
		if err := validateEncodedBitfield(value, rwiByteFlagLength); err != nil {
			return fmt.Errorf("%w %s: %w", errInvalidRWIProperty, key, err)
		}
	default:
		if width, ok := rwiCardinalWidths[key]; ok {
			return validateFixedWidthUnsignedDecimal(key, value, width)
		}
		if width, ok := rwiBinaryWidths[key]; ok {
			return validateFixedWidthUnsignedDecimal(key, value, width)
		}
	}
	return nil
}

func validateFixedWidthUnsignedDecimal(key, value string, byteWidth int) error {
	n, err := strconv.ParseUint(value, 10, byteWidth*8)
	if err != nil {
		return fmt.Errorf("%w %s: %w", errInvalidRWIProperty, key, err)
	}
	if n > maxUnsigned(byteWidth) {
		return fmt.Errorf(
			"%w %s: %d exceeds %d-byte maximum",
			errInvalidRWIProperty,
			key,
			n,
			byteWidth,
		)
	}
	return nil
}

func maxUnsigned(byteWidth int) uint64 {
	return 1<<(byteWidth*8) - 1
}
