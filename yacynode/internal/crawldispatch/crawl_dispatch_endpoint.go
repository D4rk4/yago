package crawldispatch

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/D4rk4/yago/yacymodel"
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
	initiator yacymodel.Hash
	mint      ProvenanceMint
	queue     CrawlOrderQueue
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

	var input operatorCrawlRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		e.reject(w, req, err)
		return
	}

	order, err := input.order(e.initiator, e.mint(), time.Now())
	if err != nil {
		e.reject(w, req, err)
		return
	}

	duplicate, err := e.queue.PublishOnce(req.Context(), key, order)
	if err != nil {
		slog.ErrorContext(req.Context(), msgCrawlOrderFailed, slog.Any("error", err))
		http.Error(w, "crawl order publish failed", http.StatusBadGateway)
		return
	}

	slog.InfoContext(
		req.Context(),
		msgCrawlOrderPublished,
		slog.String("profileHandle", order.Profile.Handle),
		slog.Int("seeds", len(order.Requests)),
		slog.Bool("duplicate", duplicate),
	)

	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(crawlDispatchAccepted{
		ProfileHandle: order.Profile.Handle,
		Seeds:         len(order.Requests),
		Duplicate:     duplicate,
	})
}

func (e crawlDispatchEndpoint) reject(w http.ResponseWriter, req *http.Request, err error) {
	slog.WarnContext(req.Context(), msgCrawlOrderRejected, slog.Any("error", err))
	http.Error(w, err.Error(), http.StatusBadRequest)
}
