package documentstore

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const maxExtractedTextBytes = 1 << 20

type documentVault struct {
	vault                *vault.Vault
	legacyDocuments      *vault.Keyspace[Document]
	orderedDocuments     *vault.Keyspace[Document]
	documentLocations    *vault.Keyspace[uint64]
	inboundAnchors       *vault.Keyspace[[]AnchorText]
	outboundTargets      *vault.Keyspace[[]string]
	outboundPublications *vault.Keyspace[outboundAnchorPublication]
	scanAdmission        chan struct{}
	writeBoundary        *storedDocumentWriteBoundary
	admissionKeys        *storedDocumentAdmissionKeys
	urlBoundaries        *storedDocumentURLBoundaries
	outboundBoundaries   *storedDocumentURLBoundaries
}

type stagedStoredDocument struct {
	document Document
	location storedDocumentLocation
}

func (d documentVault) Receive(ctx context.Context, docs []Document) (Receipt, error) {
	if len(docs) == 0 {
		return Receipt{}, nil
	}
	prepared := make([]Document, len(docs))
	urls := make([]string, 0, len(docs))
	for index, document := range docs {
		prepared[index] = normalizedDocument(document)
		urls = append(urls, prepared[index].NormalizedURL)
	}
	releaseWrite, err := d.enterStoredDocumentWrite(ctx)
	if err != nil {
		return Receipt{}, err
	}
	defer releaseWrite()
	releaseURLs, err := d.urlBoundaries.lockWrites(ctx, urls)
	if err != nil {
		return Receipt{}, err
	}
	defer releaseURLs()

	atCapacity, err := d.vault.AtCapacity(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("check capacity: %w", err)
	}
	if atCapacity {
		return Receipt{Busy: true}, nil
	}
	plan, err := d.planStoredDocumentWrites(ctx, prepared)
	if err != nil {
		return Receipt{}, err
	}
	receipt, err := d.store(ctx, prepared, plan)
	if err != nil {
		return Receipt{}, err
	}

	return receipt, nil
}

func (d documentVault) store(
	ctx context.Context,
	docs []Document,
	plan storedDocumentWritePlan,
) (Receipt, error) {
	var receipt Receipt
	pendingLocations := make(map[string]storedDocumentLocationPublication)
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		attempt := newStoredDocumentWriteAttempt(plan)
		for _, document := range docs {
			if err := d.storeOne(
				ctx,
				tx,
				document,
				&attempt,
			); err != nil {
				return err
			}
		}
		receipt = attempt.receipt
		pendingLocations = attempt.pendingLocations

		return nil
	})
	if err != nil {
		return Receipt{}, fmt.Errorf("store document rows: %w", err)
	}
	if err := d.publishDocumentLocations(ctx, pendingLocations); err != nil {
		return Receipt{}, d.recoverFailedDocumentPublication(
			ctx,
			pendingLocations,
			err,
		)
	}

	return receipt, nil
}

func (d documentVault) storeOne(
	ctx context.Context,
	tx *vault.Txn,
	document Document,
	attempt *storedDocumentWriteAttempt,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	document, location, accepted, found, err := d.canonicalDocument(
		tx,
		document,
		attempt.staged,
	)
	if err != nil {
		return err
	}
	if !accepted {
		attempt.receipt.Rejected++

		return nil
	}
	if !found {
		admission, planned := attempt.plan.admissions[document.NormalizedURL]
		if !planned {
			return fmt.Errorf("document admission was not reserved")
		}
		location = storedDocumentLocation{admission: admission}
		attempt.pendingLocations[document.NormalizedURL] = storedDocumentLocationPublication{
			admission:         admission,
			previousAdmission: attempt.plan.previousAdmissions[document.NormalizedURL],
		}
	}
	if err := d.putStoredDocument(tx, location, document); err != nil {
		return err
	}
	attempt.staged[document.NormalizedURL] = stagedStoredDocument{
		document: document,
		location: location,
	}
	if found {
		attempt.receipt.Updated++
	} else {
		attempt.receipt.Stored++
	}
	attempt.receipt.CommittedDocuments = append(
		attempt.receipt.CommittedDocuments,
		document,
	)

	return nil
}

func (d documentVault) publishDocumentLocations(
	ctx context.Context,
	locations map[string]storedDocumentLocationPublication,
) error {
	if len(locations) == 0 {
		return nil
	}
	urls := slices.Sorted(maps.Keys(locations))
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, normalizedURL := range urls {
			if err := d.publishDocumentLocation(
				tx,
				normalizedURL,
				locations[normalizedURL],
			); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("publish document locations: %w", err)
	}

	return nil
}

func (d documentVault) publishDocumentLocation(
	tx *vault.Txn,
	normalizedURL string,
	publication storedDocumentLocationPublication,
) error {
	current, located, err := d.documentLocations.Get(tx, vault.Key(normalizedURL))
	if err != nil {
		return fmt.Errorf("read published document location: %w", err)
	}
	if located && current == publication.admission {
		return nil
	}
	if publication.previousAdmission == 0 && located {
		return fmt.Errorf("document location changed before publication")
	}
	if publication.previousAdmission > 0 &&
		(!located || current != publication.previousAdmission) {
		return fmt.Errorf("document location changed before publication")
	}
	if err := d.documentLocations.Put(
		tx,
		vault.Key(normalizedURL),
		publication.admission,
	); err != nil {
		return fmt.Errorf("publish document location: %w", err)
	}

	return nil
}

func (d documentVault) CanonicalDocuments(
	ctx context.Context,
	docs []Document,
) ([]Document, error) {
	prepared := make([]Document, len(docs))
	urls := make([]string, 0, len(docs))
	for index, document := range docs {
		prepared[index] = normalizedDocument(document)
		urls = append(urls, prepared[index].NormalizedURL)
	}
	releaseURLs, err := d.urlBoundaries.lockReads(ctx, urls)
	if err != nil {
		return nil, err
	}
	defer releaseURLs()
	canonical := make([]Document, 0, len(docs))
	err = d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, document := range prepared {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
			}
			stored, _, accepted, _, err := d.canonicalDocument(
				tx,
				document,
				nil,
			)
			if err != nil {
				return err
			}
			if accepted {
				canonical = append(canonical, stored)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("canonical documents: %w", err)
	}

	return canonical, nil
}

func (d documentVault) canonicalDocument(
	tx *vault.Txn,
	document Document,
	staged map[string]stagedStoredDocument,
) (Document, storedDocumentLocation, bool, bool, error) {
	document = normalizedDocument(document)
	if document.NormalizedURL == "" {
		return Document{}, storedDocumentLocation{}, false, false, nil
	}
	var location storedDocumentLocation
	var previous Document
	var found bool
	if stagedDocument, stagedEarlier := staged[document.NormalizedURL]; stagedEarlier {
		previous = stagedDocument.document
		location = stagedDocument.location
		found = true
	} else {
		var err error
		previous, location, found, err = d.readStoredDocument(
			tx,
			document.NormalizedURL,
		)
		if err != nil {
			return Document{}, storedDocumentLocation{}, false, false, err
		}
	}
	document = mergeDocumentDates(previous, document, found)
	key := vault.Key(document.NormalizedURL)
	storedAnchors, anchorsFound, err := d.inboundAnchors.Get(tx, key)
	if err != nil {
		return Document{}, storedDocumentLocation{}, false, false, fmt.Errorf(
			"read inbound anchors: %w",
			err,
		)
	}
	if anchorsFound {
		document.Inlinks = canonicalAnchorTexts(append(document.Inlinks, storedAnchors...))
	}

	return document, location, true, found, nil
}

func (d documentVault) Document(
	ctx context.Context,
	normalizedURL string,
) (Document, bool, error) {
	releaseURL, err := d.urlBoundaries.lockReads(ctx, []string{normalizedURL})
	if err != nil {
		return Document{}, false, err
	}
	defer releaseURL()
	var document Document
	var found bool
	err = d.vault.View(ctx, func(tx *vault.Txn) error {
		read, _, present, err := d.readStoredDocument(tx, normalizedURL)
		document = read
		found = present

		return err
	})
	if err != nil {
		return Document{}, false, fmt.Errorf("document: %w", err)
	}

	return document, found, nil
}

func (d documentVault) DocumentExists(
	ctx context.Context,
	normalizedURL string,
) (bool, error) {
	releaseURL, err := d.urlBoundaries.lockReads(ctx, []string{normalizedURL})
	if err != nil {
		return false, err
	}
	defer releaseURL()
	var found bool
	err = d.vault.View(ctx, func(tx *vault.Txn) error {
		location, present, err := d.locateStoredDocument(tx, normalizedURL)
		if err != nil || !present {
			found = false

			return err
		}
		if location.admission == 0 {
			found = true

			return nil
		}
		key, err := orderedDocumentKey(location.admission, normalizedURL)
		if err != nil {
			return err
		}
		found = d.orderedDocuments.Contains(tx, key)

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("document presence: %w", err)
	}

	return found, nil
}

func (d documentVault) Delete(ctx context.Context, normalizedURL string) (bool, error) {
	releaseWrite, err := d.enterStoredDocumentWrite(ctx)
	if err != nil {
		return false, err
	}
	defer releaseWrite()
	releaseURL, err := d.urlBoundaries.lockWrites(ctx, []string{normalizedURL})
	if err != nil {
		return false, err
	}
	defer releaseURL()

	return d.deleteStoredDocument(ctx, normalizedURL)
}

func (d documentVault) Count(ctx context.Context) (int, error) {
	count := 0
	err := d.ScanStoredDocumentPages(
		ctx,
		func(Document) (bool, error) {
			count++

			return true, nil
		},
	)
	if err != nil {
		return 0, fmt.Errorf("document count: %w", err)
	}

	return count, nil
}

func (d documentVault) StoredDocuments(
	ctx context.Context,
	visit func(Document) (bool, error),
) error {
	if err := d.ScanStoredDocumentPages(
		ctx,
		visit,
	); err != nil {
		return fmt.Errorf("stored documents: %w", err)
	}

	return nil
}

func normalizedDocument(doc Document) Document {
	if doc.NormalizedURL == "" {
		doc.NormalizedURL = doc.CanonicalURL
	}
	if doc.CanonicalURL == "" {
		doc.CanonicalURL = doc.NormalizedURL
	}
	doc.ExtractedText = boundedText(doc.ExtractedText)
	doc.ContentSafety = normalizedContentSafetyEvidence(doc.ContentSafety)
	doc.Headings = append([]string(nil), doc.Headings...)
	doc.Outlinks = append([]string(nil), doc.Outlinks...)
	doc.Inlinks = append([]AnchorText(nil), doc.Inlinks...)
	doc.OutboundAnchors = append([]OutboundAnchor(nil), doc.OutboundAnchors...)
	doc.Images = append([]ImageMetadata(nil), doc.Images...)
	if doc.Metadata != nil {
		metadata := make(map[string]string, len(doc.Metadata))
		for key, value := range doc.Metadata {
			metadata[key] = value
		}
		doc.Metadata = metadata
	}

	return doc
}

func boundedText(text string) string {
	if len(text) <= maxExtractedTextBytes {
		return text
	}

	end := 0
	for index := range text {
		if index > maxExtractedTextBytes {
			break
		}
		end = index
	}

	return text[:end]
}

var (
	_ DocumentDirectory          = documentVault{}
	_ DocumentPresence           = documentVault{}
	_ CanonicalDocumentDirectory = documentVault{}
	_ DocumentReceiver           = documentVault{}
	_ StoredDocuments            = documentVault{}
	_ InboundAnchorReceiver      = documentVault{}
)
