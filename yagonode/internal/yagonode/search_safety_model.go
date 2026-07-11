package yagonode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/contentsafety"
	"github.com/D4rk4/yago/yagonode/internal/safetymodel"
)

const (
	pathSearchSafetyModel     = "/api/admin/v1/search/safety/model"
	pathSearchSafetyTrain     = "/api/admin/v1/search/safety/model/train"
	pathSearchSafetyRollback  = "/api/admin/v1/search/safety/model/rollback"
	maximumSafetyTrainingBody = 2 << 20
)

type safetyModelCatalog interface {
	Status() safetymodel.Status
	ActiveSnapshotJSON() []byte
	Activate(context.Context, safetymodel.Snapshot) error
	Rollback(context.Context) (bool, error)
}

type safetyModelResponse struct {
	Status         safetymodel.Status `json:"status"`
	ActiveSnapshot json.RawMessage    `json:"active_snapshot,omitempty"`
}

type searchSafetyModelEndpoint struct {
	catalog safetyModelCatalog
}

func newSearchSafetyModelEndpoint(catalog safetyModelCatalog) http.Handler {
	return searchSafetyModelEndpoint{catalog: catalog}
}

func (endpoint searchSafetyModelEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}
	if endpoint.catalog == nil {
		http.Error(w, "content safety model catalog unavailable", http.StatusServiceUnavailable)

		return
	}
	writeSafetyModelResponse(w, endpoint.catalog)
}

type safetyTrainingDocument struct {
	Text   string `json:"text"`
	Rating string `json:"rating"`
}

type safetyTrainingRequest struct {
	Revision  string                   `json:"revision"`
	Documents []safetyTrainingDocument `json:"documents"`
}

type searchSafetyTrainEndpoint struct {
	catalog safetyModelCatalog
}

func newSearchSafetyTrainEndpoint(catalog safetyModelCatalog) http.Handler {
	return searchSafetyTrainEndpoint{catalog: catalog}
}

func (endpoint searchSafetyTrainEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}
	if endpoint.catalog == nil {
		http.Error(w, "content safety model catalog unavailable", http.StatusServiceUnavailable)

		return
	}
	request, err := decodeSafetyTrainingRequest(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	documents, err := safetyTrainingDocuments(request.Documents)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	model, err := contentsafety.TrainCharacterModel(r.Context(), documents)
	if err != nil {
		http.Error(w, fmt.Sprintf("train content safety model: %v", err), http.StatusBadRequest)

		return
	}
	snapshot, err := safetymodel.NewSnapshot(strings.TrimSpace(request.Revision), model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	if err := endpoint.catalog.Activate(r.Context(), snapshot); err != nil {
		http.Error(
			w,
			fmt.Sprintf("activate content safety model: %v", err),
			http.StatusInternalServerError,
		)

		return
	}
	writeSafetyModelResponse(w, endpoint.catalog)
}

type searchSafetyRollbackEndpoint struct {
	catalog safetyModelCatalog
}

func newSearchSafetyRollbackEndpoint(catalog safetyModelCatalog) http.Handler {
	return searchSafetyRollbackEndpoint{catalog: catalog}
}

func (endpoint searchSafetyRollbackEndpoint) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}
	if endpoint.catalog == nil {
		http.Error(w, "content safety model catalog unavailable", http.StatusServiceUnavailable)

		return
	}
	rolledBack, err := endpoint.catalog.Rollback(r.Context())
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("rollback content safety model: %v", err),
			http.StatusInternalServerError,
		)

		return
	}
	if !rolledBack {
		http.Error(w, "content safety model rollback is unavailable", http.StatusConflict)

		return
	}
	writeSafetyModelResponse(w, endpoint.catalog)
}

func decodeSafetyTrainingRequest(
	w http.ResponseWriter,
	r *http.Request,
) (safetyTrainingRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maximumSafetyTrainingBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request safetyTrainingRequest
	if err := decoder.Decode(&request); err != nil {
		return safetyTrainingRequest{}, fmt.Errorf(
			"decode content safety training request: %w",
			err,
		)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return safetyTrainingRequest{}, fmt.Errorf(
				"content safety training request has trailing data",
			)
		}

		return safetyTrainingRequest{}, fmt.Errorf(
			"decode content safety training request: %w",
			err,
		)
	}

	return request, nil
}

func safetyTrainingDocuments(
	requested []safetyTrainingDocument,
) ([]contentsafety.LabeledDocument, error) {
	documents := make([]contentsafety.LabeledDocument, len(requested))
	for index, document := range requested {
		var rating contentsafety.Rating
		switch strings.ToLower(strings.TrimSpace(document.Rating)) {
		case "general":
			rating = contentsafety.General
		case "explicit":
			rating = contentsafety.Explicit
		default:
			return nil, fmt.Errorf("training document %d has an invalid rating", index+1)
		}
		documents[index] = contentsafety.LabeledDocument{Text: document.Text, Rating: rating}
	}

	return documents, nil
}

func writeSafetyModelResponse(w http.ResponseWriter, catalog safetyModelCatalog) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(safetyModelResponse{
		Status:         catalog.Status(),
		ActiveSnapshot: catalog.ActiveSnapshotJSON(),
	})
}
