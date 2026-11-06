package yagocrawlcontract

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const MaximumHTTPValidatorBytes = 1024

var (
	errHTTPValidatorsEmpty = errors.New("http validators carry no validator")
	errEntityTagTooLarge   = errors.New("entity tag exceeds size limit")
	errEntityTagSyntax     = errors.New("entity tag syntax is invalid")
	errHTTPDateSyntax      = errors.New("last-modified value is not an HTTP date")
)

var httpDateLayouts = [...]string{
	"Mon, 02 Jan 2006 15:04:05 GMT",
	"Monday, 02-Jan-06 15:04:05 GMT",
	"Mon Jan _2 15:04:05 2006",
}

type HTTPValidators struct {
	ETag         string `json:",omitempty"`
	LastModified string `json:",omitempty"`
}

func (v HTTPValidators) Validate() error {
	if v.ETag == "" && v.LastModified == "" {
		return errHTTPValidatorsEmpty
	}
	if v.ETag != "" {
		if err := validateEntityTag(v.ETag); err != nil {
			return err
		}
	}
	if v.LastModified != "" && !isHTTPDate(v.LastModified) {
		return errHTTPDateSyntax
	}

	return nil
}

func validateEntityTag(entityTag string) error {
	if len(entityTag) > MaximumHTTPValidatorBytes {
		return fmt.Errorf("%w: %d bytes", errEntityTagTooLarge, len(entityTag))
	}
	opaque := strings.TrimPrefix(entityTag, "W/")
	if len(opaque) < 2 || opaque[0] != '"' || opaque[len(opaque)-1] != '"' {
		return errEntityTagSyntax
	}
	for _, character := range []byte(opaque[1 : len(opaque)-1]) {
		if character < '!' || character == '"' || character > '~' {
			return errEntityTagSyntax
		}
	}

	return nil
}

func isHTTPDate(value string) bool {
	for _, layout := range httpDateLayouts {
		parsed, err := time.Parse(layout, value)
		if err == nil && parsed.Format(layout) == value {
			return true
		}
	}

	return false
}
