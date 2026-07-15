package documentstore

import "context"

type storedDocumentLocationPublication struct {
	admission         uint64
	previousAdmission uint64
}

type storedDocumentWritePlan struct {
	admissions         map[string]uint64
	existing           map[string]stagedStoredDocument
	previousAdmissions map[string]uint64
}

type storedDocumentWriteAttempt struct {
	plan             storedDocumentWritePlan
	staged           map[string]stagedStoredDocument
	pendingLocations map[string]storedDocumentLocationPublication
	receipt          Receipt
}

func (d documentVault) planStoredDocumentWrites(
	ctx context.Context,
	documents []Document,
) (storedDocumentWritePlan, error) {
	urls := uniqueStoredDocumentWriteURLs(documents)
	plan, missing, err := d.inspectStoredDocumentWrites(ctx, urls)
	if err != nil {
		return storedDocumentWritePlan{}, err
	}
	admissions, err := d.admissionKeys.issue(ctx, len(missing))
	if err != nil {
		return storedDocumentWritePlan{}, err
	}
	for index, normalizedURL := range missing {
		plan.admissions[normalizedURL] = admissions[index]
	}

	return plan, nil
}

func newStoredDocumentWriteAttempt(
	plan storedDocumentWritePlan,
) storedDocumentWriteAttempt {
	staged := make(map[string]stagedStoredDocument, len(plan.existing))
	for normalizedURL, document := range plan.existing {
		staged[normalizedURL] = document
	}

	return storedDocumentWriteAttempt{
		plan:             plan,
		staged:           staged,
		pendingLocations: make(map[string]storedDocumentLocationPublication),
	}
}
