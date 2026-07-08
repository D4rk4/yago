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
		return validateOptionalEncoded(key, value)
	case ColDocType, ColWordType:
		if _, err := strconv.ParseUint(value, 10, 8); err != nil {
			return fmt.Errorf("%w %s: %w", errInvalidRWIProperty, key, err)
		}
	default:
		if _, ok := rwiCardinalWidths[key]; ok {
			if _, err := strconv.ParseUint(value, 10, 64); err != nil {
				return fmt.Errorf("%w %s: %w", errInvalidRWIProperty, key, err)
			}
		}
	}
	return nil
}

func validateOptionalEncoded(key, value string) error {
	if _, err := Decode(value); err != nil {
		return fmt.Errorf("%w %s: %w", errInvalidRWIProperty, key, err)
	}

	return nil
}
