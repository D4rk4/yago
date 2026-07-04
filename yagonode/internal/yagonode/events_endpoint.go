package yagonode

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/events"
)

const (
	pathEvents         = "/api/admin/v1/events"
	defaultEventsLimit = 100
)

type eventsEndpoint struct {
	recorder *events.Recorder
	now      func() time.Time
}

type eventsResponse struct {
	GeneratedAt string         `json:"generatedAt"`
	Events      []eventPayload `json:"events"`
}

type eventPayload struct {
	Time     string `json:"time"`
	Severity string `json:"severity"`
	Category string `json:"category"`
	Name     string `json:"name"`
	Message  string `json:"message,omitempty"`
}

func newEventsEndpoint(recorder *events.Recorder) http.Handler {
	return eventsEndpoint{recorder: recorder, now: time.Now}
}

func (e eventsEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	limit, ok := eventsLimit(r.URL.Query().Get("limit"))
	if !ok {
		http.Error(w, "limit must be a positive integer", http.StatusBadRequest)

		return
	}

	recent := e.recorder.Recent(limit)
	payload := make([]eventPayload, 0, len(recent))
	for _, event := range recent {
		payload = append(payload, eventPayload{
			Time:     event.Time.UTC().Format(time.RFC3339),
			Severity: string(event.Severity),
			Category: string(event.Category),
			Name:     event.Name,
			Message:  event.Message,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(eventsResponse{
		GeneratedAt: e.now().UTC().Format(time.RFC3339),
		Events:      payload,
	})
}

func eventsLimit(raw string) (int, bool) {
	if raw == "" {
		return defaultEventsLimit, true
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, false
	}

	return value, true
}
