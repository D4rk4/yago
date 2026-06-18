package api

import (
	"context"
	"log/slog"
	"net/http"
)

const responseBadRequest = "bad request"

func failBadRequest(ctx context.Context, w http.ResponseWriter, err error) {
	slog.DebugContext(ctx, responseBadRequest, "error", err)
	http.Error(w, responseBadRequest, http.StatusBadRequest)
}

func failInternal(ctx context.Context, w http.ResponseWriter, operation string, err error) {
	slog.ErrorContext(ctx, operation, "error", err)
	http.Error(w, operation, http.StatusInternalServerError)
}
