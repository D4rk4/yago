package httpguard

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

func FailBadRequest(ctx context.Context, w http.ResponseWriter, err error) {
	slog.DebugContext(ctx, responseBadRequest, slog.Any("error", err))
	http.Error(w, responseBadRequest, http.StatusBadRequest)
}

func FailRequestTooLarge(ctx context.Context, w http.ResponseWriter, err error) {
	slog.DebugContext(ctx, responseTooLarge, slog.Any("error", err))
	http.Error(w, responseTooLarge, http.StatusRequestEntityTooLarge)
}

func FailMethodNotAllowed(ctx context.Context, w http.ResponseWriter, method string) {
	slog.DebugContext(ctx, responseMethodNotAllowed, slog.String("method", method))
	http.Error(w, responseMethodNotAllowed, http.StatusMethodNotAllowed)
}
