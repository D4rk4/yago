package yagocrawlcontract

import (
	"errors"
	"fmt"
)

var errConditionalFetchMode = errors.New("conditional fetch requires a url request")

type ConditionalFetch struct {
	Validators HTTPValidators
	Revision   DocumentRevision
}

func (f ConditionalFetch) Validate() error {
	if err := f.Validators.Validate(); err != nil {
		return fmt.Errorf("conditional fetch validators: %w", err)
	}
	if err := f.Revision.Validate(); err != nil {
		return fmt.Errorf("conditional fetch revision: %w", err)
	}

	return nil
}

func (r CrawlRequest) ValidateConditionalFetch() error {
	if r.ConditionalFetch == nil {
		return nil
	}
	if mode, _ := NormalizeCrawlRequestMode(r.Mode); mode != CrawlRequestModeURL {
		return fmt.Errorf("%w: mode %q", errConditionalFetchMode, r.Mode)
	}

	return r.ConditionalFetch.Validate()
}
