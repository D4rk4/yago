package documentstore

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type Document struct {
	CanonicalURL        string
	NormalizedURL       string
	Title               string
	Headings            []string
	ExtractedText       string
	RawContentReference string
	Language            string
	ContentType         string
	FetchStatus         string
	FetchedAt           time.Time
	IndexedAt           time.Time
	ContentHash         string
	Outlinks            []string
	Inlinks             []AnchorText
	Images              []ImageMetadata
	Metadata            map[string]string
}

type AnchorText struct {
	URL  string
	Text string
}

type ImageMetadata struct {
	URL     string
	AltText string
}

type DocumentDirectory interface {
	Document(ctx context.Context, normalizedURL string) (Document, bool, error)
	Count(ctx context.Context) (int, error)
}

type StoredDocuments interface {
	StoredDocuments(ctx context.Context, visit func(Document) (bool, error)) error
}

type DocumentReceiver interface {
	Receive(ctx context.Context, docs []Document) (Receipt, error)
}

type Receipt struct {
	Busy     bool
	Stored   int
	Updated  int
	Rejected int
}

func Open(v *vault.Vault) (DocumentDirectory, DocumentReceiver, error) {
	collection, err := registerCollection(v)
	if err != nil {
		return nil, nil, err
	}

	documents := documentVault{vault: v, collection: collection}
	return documents, documents, nil
}
