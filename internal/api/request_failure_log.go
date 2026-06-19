package api

import (
	"context"
	"log/slog"
	"net/http"
)

const (
	responseBadRequest       = "bad request"
	responseTooLarge         = "request body too large"
	responseMethodNotAllowed = "method not allowed"
)

func failBadRequest(ctx context.Context, w http.ResponseWriter, err error) {
	slog.DebugContext(ctx, responseBadRequest, "error", err)
	http.Error(w, responseBadRequest, http.StatusBadRequest)
}

func failRequestTooLarge(ctx context.Context, w http.ResponseWriter, err error) {
	slog.DebugContext(ctx, responseTooLarge, "error", err)
	http.Error(w, responseTooLarge, http.StatusRequestEntityTooLarge)
}

func failMethodNotAllowed(ctx context.Context, w http.ResponseWriter, method string) {
	slog.DebugContext(ctx, responseMethodNotAllowed, "method", method)
	http.Error(w, responseMethodNotAllowed, http.StatusMethodNotAllowed)
}

func failInternal(ctx context.Context, w http.ResponseWriter, operation string, err error) {
	slog.ErrorContext(ctx, operation, "error", err)
	http.Error(w, operation, http.StatusInternalServerError)
}
