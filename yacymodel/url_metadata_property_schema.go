package yacymodel

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	urlMetadataOpen  = '{'
	urlMetadataClose = '}'
	shortDayLength   = len("20060102")
)

var errInvalidURLMetadataProperty = errors.New("invalid url metadata property")

var urlMetadataIntegerKeys = map[string]struct{}{
	"size":   {},
	"wc":     {},
	"llocal": {},
	"lother": {},
	"limage": {},
	"laudio": {},
	"lvideo": {},
	"lapp":   {},
}

func validateURLMetadataProperties(props map[string]string) error {
	if _, err := urlMetadataHash(props); err != nil {
		return err
	}
	for key, value := range props {
		if err := validateURLMetadataProperty(key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateURLMetadataProperty(key, value string) error {
	switch key {
	case URLMetaHash, URLMetaHashAlt, "referrer":
		if value != "" {
			if _, err := ParseHash(value); err != nil {
				return fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, key, err)
			}
		}
	case "mod", "load", "fresh":
		if err := validateShortDay(key, value); err != nil {
			return err
		}
	case "dt":
		if len(value) != 1 {
			return fmt.Errorf(
				"%w %s: length %d, want 1",
				errInvalidURLMetadataProperty,
				key,
				len(value),
			)
		}
	case "lang":
		if value != "" && len(value) != langLength {
			return fmt.Errorf(
				"%w %s: length %d, want %d",
				errInvalidURLMetadataProperty,
				key,
				len(value),
				langLength,
			)
		}
	case "flags":
		if value != "" {
			if err := validateEncodedBitfield(value, rwiByteFlagLength); err != nil {
				return fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, key, err)
			}
		}
	case "url", "descr", "author", "tags", "publisher", "snippet", "favicon", "mime":
		if err := validateSimpleEncodedValue(key, value); err != nil {
			return err
		}
	}
	if _, ok := urlMetadataIntegerKeys[key]; ok {
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, key, err)
		}
	}
	return nil
}

func validateShortDay(key, value string) error {
	if len(value) != shortDayLength {
		return fmt.Errorf(
			"%w %s: length %d, want %d",
			errInvalidURLMetadataProperty,
			key,
			len(value),
			shortDayLength,
		)
	}
	if _, err := time.Parse("20060102", value); err != nil {
		return fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, key, err)
	}
	return nil
}

func validateSimpleEncodedValue(key, value string) error {
	if len(value) < 2 || value[1] != wireFormSep {
		return nil
	}
	switch value[0] {
	case wireFormPlain, wireFormBase64, wireFormGzip:
		return nil
	default:
		return fmt.Errorf(
			"%w %s: simple encoding tag %q",
			errInvalidURLMetadataProperty,
			key,
			value[0],
		)
	}
}

func urlMetadataHash(props map[string]string) (Hash, error) {
	if v, ok := props[URLMetaHash]; ok {
		hash, err := ParseHash(v)
		if err != nil {
			return "", fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, URLMetaHash, err)
		}
		return hash, nil
	}
	if v, ok := props[URLMetaHashAlt]; ok {
		hash, err := ParseHash(v)
		if err != nil {
			return "", fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, URLMetaHashAlt, err)
		}
		return hash, nil
	}
	return "", fmt.Errorf("%w: no url hash", ErrBadURLMetadata)
}
