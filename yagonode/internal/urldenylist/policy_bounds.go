package urldenylist

import (
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var errInvalidCrawlPolicy = errors.New("denylist policy exceeds crawl contract bounds")

func (s *Store) prospectiveSnapshot(tx *vault.Txn, kind Kind, value string) (Snapshot, error) {
	prospective := Snapshot{urls: map[string]struct{}{}, domains: map[string]struct{}{}}
	existing := false
	if err := s.records.Scan(tx, nil, func(key vault.Key, _ record) (bool, error) {
		entryKind, entryValue := splitKey(key)
		switch entryKind {
		case KindURL:
			prospective.urls[entryValue] = struct{}{}
		case KindDomain:
			prospective.domains[entryValue] = struct{}{}
		default:
			return false, errInvalidCrawlPolicy
		}
		if entryKind == kind && entryValue == value {
			existing = true
		}

		return true, nil
	}); err != nil {
		return Snapshot{}, fmt.Errorf("scan prospective crawl policy: %w", err)
	}
	if !existing {
		switch kind {
		case KindURL:
			prospective.urls[value] = struct{}{}
		case KindDomain:
			prospective.domains[value] = struct{}{}
		}
	}

	exactURLs := make([]string, 0, len(prospective.urls))
	for exactURL := range prospective.urls {
		if err := validateRawEntry(KindURL, exactURL); err != nil {
			return Snapshot{}, fmt.Errorf("%w: invalid retained URL", errInvalidCrawlPolicy)
		}
		exactURLs = append(exactURLs, exactURL)
	}
	domains := make([]string, 0, len(prospective.domains))
	for domain := range prospective.domains {
		if err := validateRawEntry(KindDomain, domain); err != nil {
			return Snapshot{}, fmt.Errorf("%w: invalid retained domain", errInvalidCrawlPolicy)
		}
		domains = append(domains, domain)
	}
	if _, err := yagocrawlcontract.NewCrawlURLDenylist(exactURLs, domains); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", errInvalidCrawlPolicy, err)
	}
	compiled := compileSnapshot(prospective)
	if _, err := compiled.CrawlURLDenylist(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", errInvalidCrawlPolicy, err)
	}

	return compiled, nil
}
