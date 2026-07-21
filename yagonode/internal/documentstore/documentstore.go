package documentstore

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type Document struct {
	ExtractionGeneration        uint64 `json:",omitempty"`
	CanonicalURL                string
	NormalizedURL               string
	Title                       string
	Headings                    []string
	ExtractedText               string
	ContentQuality              ContentQualityEvidence
	ContentSafety               ContentSafetyEvidence
	RawContentReference         string
	Language                    string
	ContentType                 string
	FetchStatus                 string
	FetchedAt                   time.Time
	IndexedAt                   time.Time
	PublishedAt                 time.Time
	ModifiedAt                  time.Time
	FirstSeenAt                 time.Time
	ContentChangedAt            time.Time
	DateConfidence              float64
	DateSource                  string
	ContentHash                 string
	ClusterID                   string
	RepresentativeURL           string
	Outlinks                    []string
	Inlinks                     []AnchorText
	OutboundAnchors             []OutboundAnchor
	OutboundAnchorEvidenceKnown bool
	Images                      []ImageMetadata
	Metadata                    map[string]string
	submittedInlinks            []AnchorText
	submittedInlinksKnown       bool
}

type AnchorText struct {
	URL           string
	Text          string
	NoFollow      bool
	UserGenerated bool
	Sponsored     bool
}

type OutboundAnchor struct {
	TargetURL     string
	Text          string
	NoFollow      bool
	UserGenerated bool
	Sponsored     bool
}

type OutboundAnchorSet struct {
	SourceURL string
	Anchors   []OutboundAnchor
}

type AnchorUpdate struct {
	Busy          bool
	Finalizations []OutboundAnchorFinalization
}

type OutboundAnchorFinalization struct {
	sourceURL string
	expected  outboundAnchorPublication
	desired   outboundAnchorPublication
	lease     *outboundAnchorLease
}

type InboundAnchorReceiver interface {
	ReplaceOutboundAnchors(ctx context.Context, sets []OutboundAnchorSet) (AnchorUpdate, error)
	VisitOutboundAnchorDocuments(
		ctx context.Context,
		finalizations []OutboundAnchorFinalization,
		visit func([]Document) error,
	) error
	FinalizeOutboundAnchors(ctx context.Context, finalizations []OutboundAnchorFinalization) error
	ReleaseOutboundAnchors(finalizations []OutboundAnchorFinalization)
}

type ImageMetadata struct {
	URL     string
	AltText string
}

type DocumentDirectory interface {
	Document(ctx context.Context, normalizedURL string) (Document, bool, error)
	Count(ctx context.Context) (int, error)
}

type DocumentPresence interface {
	DocumentExists(ctx context.Context, normalizedURL string) (bool, error)
}

type StoredDocuments interface {
	StoredDocuments(ctx context.Context, visit func(Document) (bool, error)) error
}

// DocumentEvictor removes a stored document by its normalized URL (the store key),
// reporting whether a document was present. It backs operator delete actions.
type DocumentEvictor interface {
	Delete(ctx context.Context, normalizedURL string) (bool, error)
}

type DocumentReceiver interface {
	Receive(ctx context.Context, docs []Document) (Receipt, error)
}

type CanonicalDocumentDirectory interface {
	CanonicalDocuments(ctx context.Context, docs []Document) ([]Document, error)
}

type Receipt struct {
	Busy               bool
	Stored             int
	Updated            int
	Rejected           int
	CommittedDocuments []Document
}

func Open(v *vault.Vault) (DocumentDirectory, DocumentReceiver, error) {
	legacyDocuments, orderedDocuments, documentLocations, documentAdmissions, err := registerDocumentCollections(
		v,
	)
	if err != nil {
		return nil, nil, err
	}
	inboundAnchors, outboundTargets, outboundPublications, err := registerAnchorCollections(v)
	if err != nil {
		return nil, nil, err
	}
	admissionKeys, err := openStoredDocumentAdmissionKeys(
		v,
		documentAdmissions,
	)
	if err != nil {
		return nil, nil, err
	}

	documents := documentVault{
		vault:                v,
		legacyDocuments:      legacyDocuments,
		orderedDocuments:     orderedDocuments,
		documentLocations:    documentLocations,
		inboundAnchors:       inboundAnchors,
		outboundTargets:      outboundTargets,
		outboundPublications: outboundPublications,
		scanAdmission:        make(chan struct{}, 1),
		writeBoundary:        newStoredDocumentWriteBoundary(),
		admissionKeys:        admissionKeys,
		urlBoundaries:        &storedDocumentURLBoundaries{},
		outboundBoundaries:   &storedDocumentURLBoundaries{},
	}
	return documents, documents, nil
}
