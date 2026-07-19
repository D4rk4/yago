package yagonode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type extractionRecrawlDispatcher interface {
	Dispatch(
		ctx context.Context,
		request crawldispatch.OperatorRequest,
		key string,
	) (crawldispatch.Accepted, error)
}

type extractionRecrawlSource struct {
	documents  documentstore.StoredDocumentBatchReader
	dispatcher extractionRecrawlDispatcher
}

func newExtractionRecrawlSource(
	documents documentstore.StoredDocumentBatchReader,
	dispatcher *crawldispatch.Dispatcher,
) adminui.ExtractionRecrawlSource {
	if documents == nil || dispatcher == nil {
		return nil
	}

	return extractionRecrawlSource{documents: documents, dispatcher: dispatcher}
}

func applyExtractionRecrawlOption(options *adminui.Options, assembled node) {
	reader, ok := assembled.docScan.(documentstore.StoredDocumentBatchReader)
	if !ok {
		return
	}
	options.ExtractionRecrawl = newExtractionRecrawlSource(
		reader,
		crawlDispatcher(assembled.crawl),
	)
}

func (source extractionRecrawlSource) QueueOutdatedExtractions(
	ctx context.Context,
	actionID string,
	continuation string,
	limit int,
) (adminui.ExtractionRecrawlResult, error) {
	result := adminui.ExtractionRecrawlResult{
		Limit:             limit,
		CurrentGeneration: yagocrawlcontract.CurrentExtractionGeneration,
		ActionID:          actionID,
	}
	batch, err := source.documents.ReadStoredDocumentBatch(ctx, continuation, limit)
	if err != nil {
		result.Continuation = continuation
		result.Partial = true
		result.Retry = true

		return result, fmt.Errorf("read extraction recrawl candidates: %w", err)
	}
	result.Examined = batch.Examined
	result.Visible = len(batch.Documents)
	result.Continuation = batch.Continuation
	result.Partial = !batch.Complete
	seeds := make([]string, 0, len(batch.Documents))
	for _, document := range batch.Documents {
		if document.ExtractionGeneration >= yagocrawlcontract.CurrentExtractionGeneration {
			result.CurrentOrNewer++

			continue
		}
		seeds = append(seeds, document.NormalizedURL)
	}
	result.Outdated = len(seeds)
	if len(seeds) == 0 {
		return result, nil
	}
	maximum := len(seeds)
	accepted, err := source.dispatcher.Dispatch(ctx, crawldispatch.OperatorRequest{
		Name:            "Extraction refresh",
		Seeds:           seeds,
		StartMode:       "url",
		Scope:           "wide",
		MaxDepth:        0,
		AllowQueryURLs:  true,
		MaxPagesPerHost: maximum,
		MaxPagesPerRun:  &maximum,
	}, extractionRecrawlDispatchKey(extractionRecrawlBatchIdentity{
		ActionID:         actionID,
		Continuation:     continuation,
		NextContinuation: batch.Continuation,
		Limit:            limit,
		Examined:         batch.Examined,
		Seeds:            seeds,
	}))
	if err != nil {
		result.Continuation = continuation
		result.Partial = true
		result.Retry = true

		return result, fmt.Errorf("dispatch extraction recrawl: %w", err)
	}
	if !accepted.Duplicate {
		result.Queued = accepted.Seeds
	} else {
		result.AlreadyQueued = accepted.Seeds
	}

	return result, nil
}

type extractionRecrawlBatchIdentity struct {
	ActionID         string
	Continuation     string
	NextContinuation string
	Limit            int
	Examined         int
	Seeds            []string
}

func extractionRecrawlDispatchKey(identity extractionRecrawlBatchIdentity) string {
	content := strings.Builder{}
	writeExtractionRecrawlIdentityField(
		&content,
		strconv.FormatUint(yagocrawlcontract.CurrentExtractionGeneration, 10),
	)
	writeExtractionRecrawlIdentityField(&content, identity.ActionID)
	writeExtractionRecrawlIdentityField(&content, identity.Continuation)
	writeExtractionRecrawlIdentityField(&content, identity.NextContinuation)
	writeExtractionRecrawlIdentityField(&content, strconv.Itoa(identity.Limit))
	writeExtractionRecrawlIdentityField(&content, strconv.Itoa(identity.Examined))
	for _, seed := range identity.Seeds {
		writeExtractionRecrawlIdentityField(&content, seed)
	}
	sum := sha256.Sum256([]byte(content.String()))

	return "extraction-refresh:" + hex.EncodeToString(sum[:])
}

func writeExtractionRecrawlIdentityField(content *strings.Builder, value string) {
	content.WriteString(strconv.Itoa(len(value)))
	content.WriteByte(':')
	content.WriteString(value)
}
