package tavilyapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maximumJSONRequestBodyBytes int64 = 64 << 10

const (
	requestTooLargeErrorCode    = "request_too_large"
	requestTooLargeErrorMessage = "request body too large"
)

func decodeJSONRequest(
	w http.ResponseWriter,
	r *http.Request,
	destination any,
) error {
	if r.ContentLength > maximumJSONRequestBodyBytes {
		return &http.MaxBytesError{Limit: maximumJSONRequestBodyBytes}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximumJSONRequestBodyBytes)
	encoded, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read JSON request: %w", err)
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		return fmt.Errorf("decode JSON request: %w", err)
	}

	return nil
}

func isJSONRequestTooLarge(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)

	return ok
}
