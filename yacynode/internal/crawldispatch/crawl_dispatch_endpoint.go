package crawldispatch

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

const (
	msgCrawlOrderRejected  = "crawl order rejected"
	msgCrawlOrderPublished = "crawl order published"
	msgCrawlOrderFailed    = "crawl order publish failed"
)

type crawlDispatchEndpoint struct {
	queue CrawlOrderQueue
}

type crawlDispatchAccepted struct {
	ProfileHandle string `json:"profileHandle"`
	Seeds         int    `json:"seeds"`
}

func (e crawlDispatchEndpoint) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input operatorCrawlRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		e.reject(w, req, err)
		return
	}

	order, err := input.order()
	if err != nil {
		e.reject(w, req, err)
		return
	}

	if err := e.queue.Publish(req.Context(), order); err != nil {
		slog.ErrorContext(req.Context(), msgCrawlOrderFailed, slog.Any("error", err))
		http.Error(w, "crawl order publish failed", http.StatusBadGateway)
		return
	}

	slog.InfoContext(
		req.Context(),
		msgCrawlOrderPublished,
		slog.String("profileHandle", order.Profile.Handle),
		slog.Int("seeds", len(order.SeedURLs)),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(crawlDispatchAccepted{
		ProfileHandle: order.Profile.Handle,
		Seeds:         len(order.SeedURLs),
	})
}

func (e crawlDispatchEndpoint) reject(w http.ResponseWriter, req *http.Request, err error) {
	slog.WarnContext(req.Context(), msgCrawlOrderRejected, slog.Any("error", err))
	http.Error(w, err.Error(), http.StatusBadRequest)
}
