package yagopdf

import (
	"context"
	"errors"
	"fmt"
)

const pdfAbandonmentCheckInterval = 1024

var ErrNoReadableText = errors.New("no readable text")

func ReadPDFContext(ctx context.Context, body []byte) (Document, error) {
	return readUntilAbandoned(ctx, body, pdfReadDocument)
}

func ReadPostScriptContext(ctx context.Context, body []byte) (Document, error) {
	return readUntilAbandoned(ctx, body, postScriptReadDocument)
}

func readUntilAbandoned(
	ctx context.Context,
	body []byte,
	read func([]byte, <-chan struct{}) (Document, bool),
) (Document, error) {
	document, readable := read(body, ctx.Done())
	if err := ctx.Err(); err != nil {
		return Document{}, fmt.Errorf("read abandoned: %w", err)
	}
	if !readable {
		return Document{}, ErrNoReadableText
	}

	return document, nil
}

func (q *pdfDecodeQuota) observeAbandonment() {
	select {
	case <-q.abandoned:
		q.remainingBytes = 0
	default:
	}
}

func (c *pdfTextCollector) observeAbandonment() {
	if c.unchecked > 0 {
		c.unchecked--

		return
	}
	c.unchecked = pdfAbandonmentCheckInterval
	select {
	case <-c.abandoned:
		c.sealed = true
	default:
	}
}
