package adminui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentimport"
)

const indexImportMaxRecords = documentimport.MaximumBatchRecords

var indexImportErrorURLPattern = regexp.MustCompile(`(?i)\b(?:https?|ftp|smb)://[^\s"'<>]+`)

func (c *Console) handleIndexImport(w http.ResponseWriter, r *http.Request) {
	if c.indexImport == nil {
		http.NotFound(w, r)
		return
	}
	request, ok := decodeIndexImportRequest(w, r)
	if !ok {
		return
	}
	results, err := c.runIndexImport(r.Context(), request)
	if err != nil {
		slog.WarnContext(r.Context(), "index import batch failed",
			slog.String("error", indexImportErrorURLPattern.ReplaceAllString(
				err.Error(), "<redacted-url>",
			)),
		)
		http.Error(
			w,
			"import batch was not completed; retry the same records",
			http.StatusServiceUnavailable,
		)
		return
	}
	writeIndexImportResponse(w, results)
}

func decodeIndexImportRequest(w http.ResponseWriter, r *http.Request) (IndexImportRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return IndexImportRequest{}, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, documentimport.MaximumRequestBytes+1))
	if err != nil {
		http.Error(w, "could not read import batch", http.StatusBadRequest)
		return IndexImportRequest{}, false
	}
	if len(body) > documentimport.MaximumRequestBytes {
		http.Error(w, "import batch exceeds size limit", http.StatusRequestEntityTooLarge)
		return IndexImportRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request IndexImportRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid import request", http.StatusBadRequest)
		return IndexImportRequest{}, false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		http.Error(w, "import request must contain one JSON value", http.StatusBadRequest)
		return IndexImportRequest{}, false
	}
	if err := validateIndexImportRequest(request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return IndexImportRequest{}, false
	}
	return request, true
}

func validateIndexImportRequest(request IndexImportRequest) error {
	if request.Version != 1 || len(request.Records) == 0 ||
		len(request.Records) > indexImportMaxRecords {
		return errors.New("unsupported version or record count")
	}
	seen := make(map[string]struct{}, len(request.Records))
	for _, record := range request.Records {
		if err := validateIndexImportRecord(record); err != nil {
			return err
		}
		if _, duplicate := seen[record.ID]; duplicate {
			return errors.New("import batch contains duplicate record IDs")
		}
		seen[record.ID] = struct{}{}
	}
	return nil
}

func (c *Console) runIndexImport(
	ctx context.Context,
	request IndexImportRequest,
) ([]IndexImportResult, error) {
	if request.DryRun {
		results := make([]IndexImportResult, len(request.Records))
		for i, record := range request.Records {
			results[i] = IndexImportResult{ID: record.ID, Status: "validated"}
		}
		return results, nil
	}
	results, err := c.indexImport.ImportDocuments(ctx, request.Records)
	if err != nil {
		return nil, fmt.Errorf("import documents: %w", err)
	}
	if len(results) != len(request.Records) {
		return nil, errors.New("importer returned an incomplete receipt")
	}
	for index, result := range results {
		if result.ID != request.Records[index].ID || (result.Status != "imported" &&
			result.Status != "already-present" && result.Status != "conflict") {
			return nil, errors.New("importer returned an invalid receipt")
		}
	}
	return results, nil
}

func validateIndexImportRecord(record IndexImportRecord) error {
	if strings.TrimSpace(record.ID) == "" || len(record.ID) > 512 {
		return errors.New("each import record requires an ID of at most 512 bytes")
	}
	if err := validateIndexImportContent(record.Document); err != nil {
		return err
	}
	if err := validateIndexImportURLs(record.Document); err != nil {
		return err
	}
	if err := validateIndexImportMetadata(record.Document); err != nil {
		return err
	}
	if record.NextVisitAt != nil && record.NextVisitAt.IsZero() {
		return errors.New("nextVisitAt must be a non-zero timestamp")
	}
	if record.NextVisitAt != nil &&
		documentimport.IsPreservableLegacyURL(record.Document.NormalizedURL) {
		return errors.New("legacy malformed URL identities cannot be scheduled for recrawl")
	}
	return nil
}

func validateIndexImportContent(doc yagocrawlcontract.DocumentIngest) error {
	if len(doc.ExtractedText) > documentimport.MaximumRecordTextBytes {
		return errors.New("document text exceeds the supported limit")
	}
	if len(doc.Title) > yagocrawlcontract.MaximumDocumentTitleBytes {
		return errors.New("document title exceeds the supported limit")
	}
	if len(doc.Headings) > documentimport.MaximumRecordHeadings {
		return errors.New("document has too many headings")
	}
	for _, heading := range doc.Headings {
		if len(heading) > documentimport.MaximumRecordHeadingBytes {
			return errors.New("document heading exceeds the supported limit")
		}
	}
	return nil
}

func validateIndexImportURLs(doc yagocrawlcontract.DocumentIngest) error {
	if doc.NormalizedURL == "" {
		return errors.New("document requires a normalized URL")
	}
	legacyURLStatus := doc.Metadata[documentimport.LegacyURLStatusMetadataKey]
	if err := documentimport.ValidateURLIdentity(doc.NormalizedURL, legacyURLStatus); err != nil {
		return errors.New("document normalized URL must be a supported absolute URL")
	}
	if doc.CanonicalURL != "" {
		if err := documentimport.ValidateURLIdentity(
			doc.CanonicalURL,
			legacyURLStatus,
		); err != nil {
			return errors.New(
				"document canonical URL must be a supported absolute URL",
			)
		}
	}
	return nil
}

func validateIndexImportMetadata(doc yagocrawlcontract.DocumentIngest) error {
	if len(doc.Outlinks) > documentimport.MaximumRecordOutlinks ||
		len(doc.Inlinks) > documentimport.MaximumRecordInlinks ||
		len(doc.OutboundAnchors) > yagocrawlcontract.MaximumDocumentAnchors ||
		len(doc.Metadata) > yagocrawlcontract.MaximumDocumentMetadata {
		return errors.New("document exceeds a supported link or metadata count")
	}
	metadataBytes := 0
	for key, value := range doc.Metadata {
		metadataBytes += len(key) + len(value)
	}
	if metadataBytes > documentimport.MaximumRecordMetadataBytes {
		return errors.New("document metadata exceeds the supported size")
	}
	return nil
}

func writeIndexImportResponse(w http.ResponseWriter, results []IndexImportResult) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		Results []IndexImportResult `json:"results"`
	}{Results: results})
}
