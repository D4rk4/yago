package adminui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"time"
)

type RWIBackfillStatus struct {
	State               string    `json:"state"`
	ScannedDocuments    int64     `json:"scannedDocuments"`
	IndexedDocuments    int64     `json:"indexedDocuments"`
	SkippedDocuments    int64     `json:"skippedDocuments"`
	PostingRows         int64     `json:"postingRows"`
	EstimatedBytes      int64     `json:"estimatedBytes"`
	MaximumDocumentsRun int64     `json:"maximumDocumentsRun"`
	DryRun              bool      `json:"dryRun"`
	LastError           string    `json:"lastError,omitempty"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type RWIBackfillController interface {
	Start(context.Context, int64, bool) (RWIBackfillStatus, error)
	Pause(context.Context) (RWIBackfillStatus, error)
	Resume(context.Context, int64, bool) (RWIBackfillStatus, error)
	Status(context.Context) (RWIBackfillStatus, error)
}

type rwiBackfillRequest struct {
	Version          int    `json:"version"`
	Action           string `json:"action"`
	MaximumDocuments int64  `json:"maximumDocuments,omitempty"`
	DryRun           bool   `json:"dryRun,omitempty"`
}

func (c *Console) handleRWIBackfillStatus(w http.ResponseWriter, r *http.Request) {
	if c.rwiBackfill == nil {
		http.NotFound(w, r)
		return
	}
	status, err := c.rwiBackfill.Status(r.Context())
	if err != nil {
		http.Error(w, "backfill status is unavailable", http.StatusServiceUnavailable)
		return
	}
	writeRWIBackfillStatus(w, status)
}

func (c *Console) handleRWIBackfillControl(w http.ResponseWriter, r *http.Request) {
	if c.rwiBackfill == nil {
		http.NotFound(w, r)
		return
	}
	request, ok := decodeRWIBackfillRequest(w, r)
	if !ok {
		return
	}
	var status RWIBackfillStatus
	var err error
	switch request.Action {
	case "start":
		status, err = c.rwiBackfill.Start(r.Context(), request.MaximumDocuments, request.DryRun)
	case "pause":
		status, err = c.rwiBackfill.Pause(r.Context())
	case "resume":
		status, err = c.rwiBackfill.Resume(r.Context(), request.MaximumDocuments, request.DryRun)
	default:
		http.Error(w, "unsupported backfill action", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "backfill action was not accepted", http.StatusConflict)
		return
	}
	writeRWIBackfillStatus(w, status)
}

func decodeRWIBackfillRequest(w http.ResponseWriter, r *http.Request) (rwiBackfillRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return rwiBackfillRequest{}, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(body) > 4096 {
		http.Error(w, "backfill request exceeds size limit", http.StatusRequestEntityTooLarge)
		return rwiBackfillRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request rwiBackfillRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid backfill request", http.StatusBadRequest)
		return rwiBackfillRequest{}, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "backfill request must contain one JSON value", http.StatusBadRequest)
		return rwiBackfillRequest{}, false
	}
	if request.Version != 1 || request.MaximumDocuments < 0 ||
		request.MaximumDocuments > 10_000_000 {
		http.Error(w, "unsupported backfill request version or limit", http.StatusBadRequest)
		return rwiBackfillRequest{}, false
	}
	if request.Action == "pause" && (request.MaximumDocuments != 0 || request.DryRun) {
		http.Error(w, "pause does not accept run limits", http.StatusBadRequest)
		return rwiBackfillRequest{}, false
	}
	return request, true
}

func writeRWIBackfillStatus(w http.ResponseWriter, status RWIBackfillStatus) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		return
	}
}
