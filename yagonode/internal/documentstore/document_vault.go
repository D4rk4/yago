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
	vault                  *vault.Vault
	legacyDocuments        *vault.Keyspace[Document]
	orderedDocuments       *vault.Keyspace[Document]
	documentLocations      *vault.Keyspace[uint64]
	inboundAnchors         *vault.Keyspace[[]AnchorText]
	outboundTargets        *vault.Keyspace[[]string]
	outboundPublications   *vault.Keyspace[outboundAnchorPublication]
	inboundAnchorRefreshes *vault.Keyspace[inboundAnchorRefresh]
	scanAdmission          chan struct{}
	writeBoundary          *storedDocumentWriteBoundary
	admissionKeys          *storedDocumentAdmissionKeys
	urlBoundaries          *storedDocumentURLBoundaries
	outboundBoundaries     *storedDocumentURLBoundaries
}

type stagedStoredDocument struct {
	document Document
	location storedDocumentLocation
}

func (d documentVault) Receive(ctx context.Context, docs []Document) (Receipt, error) {
	if len(docs) == 0 {
		return Receipt{}, nil
	}
	prepared, urls := normalizedSubmissions(docs)
	atCapacity, err := d.vault.AtCapacity(ctx)
	if err != nil {
		return Receipt{}, fmt.Errorf("check capacity: %w", err)
	}
	if atCapacity {
		return Receipt{Busy: true}, nil
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
	revision, err := d.canonicalDocument(
		tx,
		document,
		attempt.staged,
	)
	if err != nil {
		return err
	}
	if revision.kind == documentRevisionRejected {
		attempt.receipt.Rejected++

		return nil
	}
	document = revision.document
	location := revision.location
	if revision.kind == documentRevisionFirst {
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
	if revision.kind != documentRevisionUnchanged {
		if err := d.putStoredDocument(tx, location, document); err != nil {
			return err
		}
	}
	attempt.staged[document.NormalizedURL] = stagedStoredDocument{
		document: document,
		location: location,
	}
	if revision.kind == documentRevisionFirst {
		attempt.receipt.Stored++
	} else {
		attempt.receipt.Updated++
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
	submissions, normalizedURLs := normalizedSubmissions(docs)
	releaseURLs, err := d.urlBoundaries.lockReads(ctx, normalizedURLs)
	if err != nil {
		return nil, err
	}
	defer releaseURLs()

	return d.acceptedCanonicalDocuments(ctx, submissions, "canonical documents")
}

func (d documentVault) canonicalDocument(
	tx *vault.Txn,
	document Document,
	staged map[string]stagedStoredDocument,
) (canonicalDocumentRevision, error) {
	document = restoreSubmittedInlinks(normalizedDocument(document))
	if document.NormalizedURL == "" {
		return canonicalDocumentRevision{kind: documentRevisionRejected}, nil
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
			return canonicalDocumentRevision{}, err
		}
	}
	document = mergeDocumentDates(previous, document, found)
	key := vault.Key(document.NormalizedURL)
	storedAnchors, anchorsFound, err := d.inboundAnchors.Get(tx, key)
	if err != nil {
		return canonicalDocumentRevision{}, fmt.Errorf(
			"read inbound anchors: %w",
			err,
		)
	}
	if anchorsFound {
		document.Inlinks = append(document.Inlinks, storedAnchors...)
	}
	document.Inlinks = canonicalAnchorTexts(document.Inlinks)

	return classifiedDocumentRevision(previous, found, document, location), nil
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
	found, err := d.DocumentsExist(ctx, []string{normalizedURL})
	if err != nil {
		return false, err
	}

	return found[0], nil
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
	doc.submittedInlinks = append([]AnchorText(nil), doc.submittedInlinks...)
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
