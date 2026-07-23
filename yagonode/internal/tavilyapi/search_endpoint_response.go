package tavilyapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

func writeSearchEndpointResponse(
	w http.ResponseWriter,
	response SearchResponse,
	err error,
	requestID string,
) {
	if err != nil {
		status, code := rawContentResponseError(
			err,
			"search_failed",
			"invalid_search_request",
		)
		if errors.Is(err, errSearchUnavailable) {
			w.Header().Set("Retry-After", "1")
		}
		writeError(w, status, code, err.Error(), requestID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
