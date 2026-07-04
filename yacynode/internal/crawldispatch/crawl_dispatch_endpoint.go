package crawldispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

const (
	msgCrawlOrderRejected  = "crawl order rejected"
	msgCrawlOrderPublished = "crawl order published"
	msgCrawlOrderFailed    = "crawl order publish failed"

	// IdempotencyKeyHeader carries an operator-chosen key that makes a repeated
	// crawl-start request enqueue at most one order.
	IdempotencyKeyHeader = "Idempotency-Key"
	maxIdempotencyKeyLen = 200
)

type crawlDispatchEndpoint struct {
	dispatcher *Dispatcher
}

type crawlDispatchAccepted struct {
	ProfileHandle string `json:"profileHandle"`
	Seeds         int    `json:"seeds"`
	Duplicate     bool   `json:"duplicate,omitempty"`
}

func (e crawlDispatchEndpoint) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	key := strings.TrimSpace(req.Header.Get(IdempotencyKeyHeader))
	if len(key) > maxIdempotencyKeyLen {
		e.reject(
			w,
			req,
			fmt.Errorf("idempotency key must not exceed %d characters", maxIdempotencyKeyLen),
		)

		return
	}

	var input OperatorRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		e.reject(w, req, err)

		return
	}

	accepted, err := e.dispatcher.Dispatch(req.Context(), input, key)
	if err != nil {
		e.fail(w, req, err)

		return
	}

	slog.InfoContext(
		req.Context(),
		msgCrawlOrderPublished,
		slog.String("profileHandle", accepted.ProfileHandle),
		slog.Int("seeds", accepted.Seeds),
		slog.Bool("duplicate", accepted.Duplicate),
	)

	status := http.StatusAccepted
	if accepted.Duplicate {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(crawlDispatchAccepted(accepted))
}

func (e crawlDispatchEndpoint) fail(w http.ResponseWriter, req *http.Request, err error) {
	var dispatchErr *DispatchError
	if errors.As(err, &dispatchErr) && dispatchErr.Retryable {
		slog.ErrorContext(req.Context(), msgCrawlOrderFailed, slog.Any("error", err))
		http.Error(w, "crawl order publish failed", http.StatusBadGateway)

		return
	}

	e.reject(w, req, err)
}

func (e crawlDispatchEndpoint) reject(w http.ResponseWriter, req *http.Request, err error) {
	slog.WarnContext(req.Context(), msgCrawlOrderRejected, slog.Any("error", err))
	http.Error(w, err.Error(), http.StatusBadRequest)
}
