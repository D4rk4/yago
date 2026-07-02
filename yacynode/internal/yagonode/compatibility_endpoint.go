package yagonode

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/compatibility"
)

const pathCompatibility = "/api/admin/v1/compatibility"

type compatibilityEndpoint struct {
	now func() time.Time
}

type compatibilityResponse struct {
	GeneratedAt string                  `json:"generatedAt"`
	Surfaces    []compatibility.Surface `json:"surfaces"`
	Counts      []compatibility.Count   `json:"counts"`
}

func newCompatibilityEndpoint() http.Handler {
	return compatibilityEndpoint{now: time.Now}
}

func (e compatibilityEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	report := compatibility.Catalog()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(compatibilityResponse{
		GeneratedAt: e.now().UTC().Format(time.RFC3339),
		Surfaces:    report.Surfaces,
		Counts:      report.Counts,
	})
}
