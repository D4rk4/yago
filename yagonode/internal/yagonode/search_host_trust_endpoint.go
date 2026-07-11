package yagonode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
)

const (
	pathSearchHostTrust         = "/api/admin/v1/search/ranking/trust"
	maximumHostTrustRequestBody = 1 << 17
)

type trustedDomainCatalog interface {
	Current() hosttrust.Policy
	Replace(context.Context, hosttrust.Policy) error
}

type searchHostTrustEndpoint struct {
	catalog trustedDomainCatalog
}

type searchHostTrustResponse struct {
	Policy hosttrust.Policy `json:"policy"`
}

func newSearchHostTrustEndpoint(catalog *hosttrust.Catalog) http.Handler {
	if catalog == nil {
		return searchHostTrustEndpoint{}
	}

	return searchHostTrustEndpoint{catalog: catalog}
}

func (endpoint searchHostTrustEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if endpoint.catalog == nil {
		http.Error(w, "host trust catalog unavailable", http.StatusServiceUnavailable)

		return
	}
	switch r.Method {
	case http.MethodGet:
		endpoint.writePolicy(w)
	case http.MethodPut:
		endpoint.replacePolicy(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (endpoint searchHostTrustEndpoint) replacePolicy(w http.ResponseWriter, r *http.Request) {
	policy, err := decodeHostTrustPolicy(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	if err := endpoint.catalog.Replace(r.Context(), policy); err != nil {
		http.Error(w, fmt.Sprintf("apply host trust policy: %v", err), http.StatusBadRequest)

		return
	}
	endpoint.writePolicy(w)
}

func (endpoint searchHostTrustEndpoint) writePolicy(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(searchHostTrustResponse{Policy: endpoint.catalog.Current()})
}

func decodeHostTrustPolicy(w http.ResponseWriter, r *http.Request) (hosttrust.Policy, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maximumHostTrustRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var policy hosttrust.Policy
	if err := decoder.Decode(&policy); err != nil {
		return hosttrust.Policy{}, fmt.Errorf("decode host trust policy: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return hosttrust.Policy{}, fmt.Errorf("host trust policy has trailing data")
	}

	return policy, nil
}
