package adminauth

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"
)

const (
	PathLogin   = "/api/admin/v1/auth/login"
	PathLogout  = "/api/admin/v1/auth/logout"
	PathSetup   = "/api/admin/v1/auth/setup"
	PathSession = "/api/admin/v1/auth/session"
)

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Username  string    `json:"username"`
	CSRFToken string    `json:"csrfToken"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type setupResponse struct {
	Username string `json:"username"`
}

type sessionResponse struct {
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func Mount(mux *http.ServeMux, service *Service) {
	mux.HandleFunc(PathLogin, service.handleLogin)
	mux.HandleFunc(PathLogout, service.handleLogout)
	mux.HandleFunc(PathSetup, service.handleSetup)
	mux.HandleFunc(PathSession, service.handleSession)
	mountAPIKeys(mux, service)
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)

		return
	}
	req, ok := decodeCredentials(w, r)
	if !ok {
		return
	}

	caller := clientIP(r)
	if !s.limiter.allow(caller) {
		s.observer.LoginThrottled()
		writeError(w, http.StatusTooManyRequests, "too many login attempts, try again later")

		return
	}

	valid, err := s.creds.verify(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login failed")

		return
	}
	if !valid {
		s.limiter.recordFailure(caller)
		s.observer.LoginFailed()
		writeError(w, http.StatusUnauthorized, "invalid username or password")

		return
	}
	s.limiter.reset(caller)

	created, err := s.sessions.create(r.Context(), req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login failed")

		return
	}
	s.observer.LoginSucceeded()
	http.SetCookie(w, sessionCookie(created.Token, r.TLS != nil, created.ExpiresAt))
	writeJSON(w, http.StatusOK, loginResponse{
		Username:  created.Username,
		CSRFToken: created.CSRFToken,
		ExpiresAt: created.ExpiresAt,
	})
}

func (s *Service) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)

		return
	}
	req, ok := decodeCredentials(w, r)
	if !ok {
		return
	}

	err := s.creds.createIfAbsent(r.Context(), req.Username, req.Password)
	if errors.Is(err, errAdminExists) {
		writeError(w, http.StatusConflict, "an administrator already exists")

		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "setup failed")

		return
	}
	writeJSON(w, http.StatusCreated, setupResponse{Username: req.Username})
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)

		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if delErr := s.sessions.delete(r.Context(), cookie.Value); delErr != nil {
			writeError(w, http.StatusInternalServerError, "logout failed")

			return
		}
	}
	http.SetCookie(w, clearedSessionCookie(r.TLS != nil))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)

		return
	}
	record, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")

		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{
		Username:  record.Username,
		ExpiresAt: record.ExpiresAt,
	})
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (credentialsRequest, bool) {
	var req credentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return credentialsRequest{}, false
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")

		return credentialsRequest{}, false
	}

	return req, true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
