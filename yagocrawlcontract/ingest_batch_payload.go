package yagocrawlcontract

import (
	"errors"
	"fmt"
	"reflect"
)

var (
	errIngestPayloadMixed = errors.New(
		"ingest batch mixes removal, document, and not-modified payloads",
	)
	errNotModifiedIdentity = errors.New(
		"not-modified observation lacks source URL or observation ID",
	)
)

type NotModifiedObservation struct {
	Revision DocumentRevision
}

func (b IngestBatch) ValidatePayload() error {
	payloads := 0
	for _, carried := range []bool{b.Removed, b.carriesDocument(), b.NotModified != nil} {
		if carried {
			payloads++
		}
	}
	if payloads > 1 {
		return errIngestPayloadMixed
	}
	if b.ResponseValidators != nil {
		if err := b.ResponseValidators.Validate(); err != nil {
			return fmt.Errorf("response validators: %w", err)
		}
	}
	if b.NotModified == nil {
		return nil
	}
	if b.SourceURL == "" || b.ObservationID == "" {
		return errNotModifiedIdentity
	}
	if err := b.NotModified.Revision.Validate(); err != nil {
		return fmt.Errorf("not-modified revision: %w", err)
	}

	return nil
}

func (b IngestBatch) carriesDocument() bool {
	return len(b.Postings) > 0 || len(b.Metadata) > 0 ||
		!withoutContent(reflect.ValueOf(b.Document))
}

func withoutContent(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Slice, reflect.Map:
		return value.Len() == 0
	case reflect.Struct:
		for index := range value.NumField() {
			if !withoutContent(value.Field(index)) {
				return false
			}
		}

		return true
	default:
		return value.IsZero()
	}
}
