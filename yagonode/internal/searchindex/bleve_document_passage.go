package searchindex

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func (b *BleveMemoryIndex) DocumentPassage(
	ctx context.Context,
	req DocumentPassageRequest,
) (DocumentPassage, bool, error) {
	if err := ctx.Err(); err != nil {
		return DocumentPassage{}, false, fmt.Errorf("document passage: %w", err)
	}
	b.mu.RLock()
	doc, found := b.documents[req.DocumentID]
	b.mu.RUnlock()

	return mappedDocumentPassage(ctx, doc, found, req)
}

func (b *BleveDiskIndex) DocumentPassage(
	ctx context.Context,
	req DocumentPassageRequest,
) (DocumentPassage, bool, error) {
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return DocumentPassage{}, false, fmt.Errorf("search index closed")
	}
	doc, found, err := b.documents.Document(ctx, req.DocumentID)
	if err != nil {
		return DocumentPassage{}, false, fmt.Errorf("load passage document: %w", err)
	}

	return mappedDocumentPassage(ctx, doc, found, req)
}

func mappedDocumentPassage(
	ctx context.Context,
	doc documentstore.Document,
	found bool,
	req DocumentPassageRequest,
) (DocumentPassage, bool, error) {
	if !found {
		return DocumentPassage{}, false, nil
	}
	passage, err := documentPassage(ctx, doc, req)
	if err != nil {
		return DocumentPassage{}, false, err
	}

	return passage, true, nil
}
