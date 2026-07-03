package adminauth

import (
	"encoding/json"
	"net/http"
	"time"
)

const (
	PathAPIKeys    = "/api/admin/v1/auth/api-keys"      //nolint:gosec // G101: URL path, not a credential value.
	pathAPIKeyByID = "/api/admin/v1/auth/api-keys/{id}" //nolint:gosec // G101: URL path, not a credential value.
)

type createAPIKeyRequest struct {
	Label  string   `json:"label"`
	Scopes []string `json:"scopes"`
}

type createAPIKeyResponse struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Scopes    []Scope   `json:"scopes"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
}

type apiKeyView struct {
	ID         string     `json:"id"`
	Scopes     []Scope    `json:"scopes"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

type listAPIKeysResponse struct {
	Keys []apiKeyView `json:"keys"`
}

func mountAPIKeys(mux *http.ServeMux, service *Service) {
	mux.HandleFunc(PathAPIKeys, service.handleAPIKeys)
	mux.HandleFunc(http.MethodDelete+" "+pathAPIKeyByID, service.handleAPIKeyRevoke)
}

func (s *Service) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAPIKeys(w, r)
	case http.MethodPost:
		s.createAPIKey(w, r)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (s *Service) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}
	scopes, err := parseScopes(req.Scopes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}
	created, err := s.apiKeys.create(r.Context(), req.Label, scopes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create API key")

		return
	}
	writeJSON(w, http.StatusCreated, createAPIKeyResponse(created))
}

func (s *Service) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.apiKeys.list(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list API keys")

		return
	}
	views := make([]apiKeyView, 0, len(keys))
	for _, key := range keys {
		views = append(views, viewFromInfo(key))
	}
	writeJSON(w, http.StatusOK, listAPIKeysResponse{Keys: views})
}

func (s *Service) handleAPIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.apiKeys.delete(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not revoke API key")

		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "API key not found")

		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func viewFromInfo(info apiKeyInfo) apiKeyView {
	view := apiKeyView{
		ID:        info.ID,
		Scopes:    info.Scopes,
		Label:     info.Label,
		CreatedAt: info.CreatedAt,
	}
	if !info.LastUsedAt.IsZero() {
		lastUsed := info.LastUsedAt
		view.LastUsedAt = &lastUsed
	}

	return view
}
