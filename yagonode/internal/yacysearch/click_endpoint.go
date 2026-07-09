package yacysearch

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// pathSearchClick receives the result-click beacon from the public results page.
// It is a yago-local endpoint, not a YaCy servlet, so it lives outside the
// yagoproto path vocabulary.
const pathSearchClick = "/searchclick"

// ClickRecorder persists a result click so the ranking learner can mine implicit
// relevance judgments from real usage; the node supplies the clickcapture store.
type ClickRecorder interface {
	Record(ctx context.Context, query, target string, rank int) error
}

// ClickCapture configures result-click capture on the results page: whether it
// is enabled and where captured clicks are recorded.
type ClickCapture struct {
	Enabled  bool
	Recorder ClickRecorder
}

// clickEndpoint records the clicks beaconed from the results page. It answers 204
// whether or not recording succeeds, because the beacon is a best-effort signal
// (navigator.sendBeacon) that must never disturb the user's navigation; only a
// malformed request is rejected so the store is not fed junk.
type clickEndpoint struct {
	recorder ClickRecorder
}

func (e clickEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	query := r.PostForm.Get("q")
	target := r.PostForm.Get("u")
	if query == "" || !isHTTPURL(target) {
		http.Error(w, "missing query or url", http.StatusBadRequest)
		return
	}
	rank, _ := strconv.Atoi(r.PostForm.Get("p"))
	_ = e.recorder.Record(r.Context(), query, target, rank)
	w.WriteHeader(http.StatusNoContent)
}

// isHTTPURL reports whether raw is an absolute http(s) URL, so the click store
// never records relative paths or javascript:/data: schemes beaconed by a
// misbehaving or hostile client.
func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}

	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
