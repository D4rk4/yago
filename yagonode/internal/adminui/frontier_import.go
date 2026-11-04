package adminui

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const crawlOrderImportPath = "/admin/crawl/frontier/import"

type crawlOrderImportResponse struct {
	Validated        bool `json:"validated,omitempty"`
	EnqueuedRequests int  `json:"enqueuedRequests"`
	AlreadyIndexed   int  `json:"alreadyIndexed"`
	DuplicateBatch   bool `json:"duplicateBatch,omitempty"`
}

func (c *Console) handleCrawlOrderImport(w http.ResponseWriter, r *http.Request) {
	if c.crawlOrderImport == nil {
		http.NotFound(w, r)
		return
	}
	batch, ok := decodeCrawlOrderImport(w, r)
	if !ok {
		return
	}
	if batch.DryRun {
		writeCrawlOrderImportResponse(w, crawlOrderImportResponse{Validated: true})
		return
	}
	result, err := c.crawlOrderImport.ImportCrawlOrder(r.Context(), batch)
	if err != nil {
		http.Error(
			w,
			"frontier batch was not completed; retry the same batch",
			http.StatusServiceUnavailable,
		)
		return
	}
	writeCrawlOrderImportResponse(w, crawlOrderImportResponse{
		EnqueuedRequests: result.EnqueuedRequests,
		AlreadyIndexed:   result.AlreadyIndexed,
		DuplicateBatch:   result.DuplicateBatch,
	})
}

func decodeCrawlOrderImport(w http.ResponseWriter, r *http.Request) (CrawlOrderImportBatch, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return CrawlOrderImportBatch{}, false
	}
	body, err := io.ReadAll(
		io.LimitReader(r.Body, yagocrawlcontract.MaximumCrawlOrderImportBatchBytes+1),
	)
	if err != nil {
		http.Error(w, "could not read frontier batch", http.StatusBadRequest)
		return CrawlOrderImportBatch{}, false
	}
	if len(body) > yagocrawlcontract.MaximumCrawlOrderImportBatchBytes {
		http.Error(w, "frontier batch exceeds size limit", http.StatusRequestEntityTooLarge)
		return CrawlOrderImportBatch{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var batch CrawlOrderImportBatch
	if err := decoder.Decode(&batch); err != nil {
		http.Error(w, "invalid frontier batch", http.StatusBadRequest)
		return CrawlOrderImportBatch{}, false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		http.Error(w, "frontier request must contain one JSON value", http.StatusBadRequest)
		return CrawlOrderImportBatch{}, false
	}
	if err := batch.Validate(); err != nil {
		http.Error(w, "invalid frontier batch", http.StatusBadRequest)
		return CrawlOrderImportBatch{}, false
	}
	return batch, true
}

func writeCrawlOrderImportResponse(w http.ResponseWriter, response crawlOrderImportResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
