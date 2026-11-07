package recrawlfrontier

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type ValidatorBinding struct {
	DocumentURL string
	Validators  yagocrawlcontract.HTTPValidators
	Revision    yagocrawlcontract.DocumentRevision
	ObservedAt  time.Time
}

type UnchangedPage struct {
	URL              string
	ProfileHandle    string
	Revision         yagocrawlcontract.DocumentRevision
	Validators       *yagocrawlcontract.HTTPValidators
	ObservedAt       time.Time
	SourceModifiedAt time.Time
}

func (b ValidatorBinding) usable() bool {
	return b.DocumentURL != "" && !b.ObservedAt.IsZero() &&
		b.Validators.Validate() == nil && b.Revision.Validate() == nil
}

func (r scheduleRecord) hasUsableBinding() bool {
	return r.ValidatorBinding != nil && r.ValidatorBinding.usable()
}

func scheduleKey(url string) vault.Key {
	hash, _ := yagomodel.HashURL(url)

	return vault.Key(string(hash))
}

func (f *Frontier) BindHTTPValidators(
	ctx context.Context,
	url string,
	binding ValidatorBinding,
) error {
	if err := f.vault.Update(ctx, func(tx *vault.Txn) error {
		key := scheduleKey(url)
		record, found, err := f.records.Get(tx, key)
		if err != nil {
			return fmt.Errorf("read recrawl record: %w", err)
		}
		if !found {
			return nil
		}
		record.ValidatorBinding = nil
		if binding.usable() {
			record.ValidatorBinding = &binding
		}
		if err := f.records.Put(tx, key, record); err != nil {
			return fmt.Errorf("write recrawl record: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("bind http validators: %w", err)
	}

	return nil
}

func (f *Frontier) ValidatorBinding(
	ctx context.Context,
	url string,
) (ValidatorBinding, bool, error) {
	var record scheduleRecord
	var found bool
	if err := f.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		record, found, err = f.records.Get(tx, scheduleKey(url))
		if err != nil {
			return fmt.Errorf("read recrawl record: %w", err)
		}

		return nil
	}); err != nil {
		return ValidatorBinding{}, false, fmt.Errorf("read validator binding: %w", err)
	}
	if !found || !record.hasUsableBinding() {
		return ValidatorBinding{}, false, nil
	}

	return *record.ValidatorBinding, true, nil
}

func (f *Frontier) RecordUnchangedPage(ctx context.Context, page UnchangedPage) (bool, error) {
	recorded := false
	if err := f.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		recorded, err = f.recordUnchangedPageInTx(tx, page)

		return err
	}); err != nil {
		return false, fmt.Errorf("record unchanged page: %w", err)
	}

	return recorded, nil
}

func (f *Frontier) recordUnchangedPageInTx(tx *vault.Txn, page UnchangedPage) (bool, error) {
	key := scheduleKey(page.URL)
	record, found, err := f.records.Get(tx, key)
	if err != nil {
		return false, fmt.Errorf("read recrawl record: %w", err)
	}
	if !found || !unchangedPageMatches(record, page) {
		return false, nil
	}
	binding := *record.ValidatorBinding
	if page.ObservedAt.Equal(binding.ObservedAt) {
		return true, nil
	}
	if _, _, err := f.clearDue(tx, key); err != nil {
		return false, err
	}
	record.NextDueAt, record.SourceModifiedAt = nextRecrawlDue(
		record,
		true,
		page.ObservedAt,
		page.SourceModifiedAt,
		record.Interval,
	)
	binding.ObservedAt = page.ObservedAt
	if page.Validators != nil && page.Validators.Validate() == nil {
		binding.Validators = *page.Validators
	}
	record.ValidatorBinding = &binding

	return true, f.schedule(tx, key, record)
}

func unchangedPageMatches(record scheduleRecord, page UnchangedPage) bool {
	binding := record.ValidatorBinding

	return record.hasUsableBinding() &&
		record.ProfileHandle == page.ProfileHandle &&
		binding.Revision.Matches(page.Revision) &&
		!page.ObservedAt.Before(binding.ObservedAt)
}
