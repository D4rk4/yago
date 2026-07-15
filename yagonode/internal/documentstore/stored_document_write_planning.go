package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func uniqueStoredDocumentWriteURLs(documents []Document) []string {
	urls := make([]string, 0, len(documents))
	seen := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		if document.NormalizedURL == "" {
			continue
		}
		if _, exists := seen[document.NormalizedURL]; exists {
			continue
		}
		seen[document.NormalizedURL] = struct{}{}
		urls = append(urls, document.NormalizedURL)
	}

	return urls
}

func (d documentVault) inspectStoredDocumentWrites(
	ctx context.Context,
	urls []string,
) (storedDocumentWritePlan, []string, error) {
	plan := storedDocumentWritePlan{
		admissions:         make(map[string]uint64),
		existing:           make(map[string]stagedStoredDocument),
		previousAdmissions: make(map[string]uint64),
	}
	missing := make([]string, 0, len(urls))
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, normalizedURL := range urls {
			document, location, found, err := d.readStoredDocument(tx, normalizedURL)
			if err != nil {
				return fmt.Errorf("read planned document: %w", err)
			}
			if found {
				plan.existing[normalizedURL] = stagedStoredDocument{
					document: document,
					location: location,
				}

				continue
			}
			if _, err := orderedDocumentKey(1, normalizedURL); err != nil {
				return fmt.Errorf("validate new document location: %w", err)
			}
			missing = append(missing, normalizedURL)
			if location.admission > 0 {
				plan.previousAdmissions[normalizedURL] = location.admission
			}
		}

		return nil
	})
	if err != nil {
		return storedDocumentWritePlan{}, nil, fmt.Errorf("plan document writes: %w", err)
	}

	return plan, missing, nil
}
