package yagomodel

import (
	"errors"
	"fmt"
	"strconv"
)

const (
	urlMetadataOpen  = '{'
	urlMetadataClose = '}'
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
	case "dt":
		if value == "" {
			return fmt.Errorf(
				"%w %s: empty",
				errInvalidURLMetadataProperty,
				key,
			)
		}
	case "flags":
		if value != "" {
			if _, err := Decode(value); err != nil {
				return fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, key, err)
			}
		}
	}
	if _, ok := urlMetadataIntegerKeys[key]; ok {
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, key, err)
		}
	}
	return nil
}

func urlMetadataHash(props map[string]string) (URLHash, error) {
	if v, ok := props[URLMetaHash]; ok {
		hash, err := ParseURLHash(v)
		if err != nil {
			return "", fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, URLMetaHash, err)
		}
		return hash, nil
	}
	if v, ok := props[URLMetaHashAlt]; ok {
		hash, err := ParseURLHash(v)
		if err != nil {
			return "", fmt.Errorf("%w %s: %w", errInvalidURLMetadataProperty, URLMetaHashAlt, err)
		}
		return hash, nil
	}
	return "", fmt.Errorf("%w: no url hash", ErrBadURLMetadata)
}
