package adminauth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

const (
	csrfHeader      = "X-CSRF-Token"
	csrfFormField   = "csrf_token"
	formContentType = "application/x-www-form-urlencoded"
	acceptHeader    = "Accept"
	contentType     = "Content-Type"
	htmlMediaType   = "text/html"
	bearerScheme    = "Bearer "
	authzHeader     = "Authorization"
)

type contextKey int

const sessionContextKey contextKey = iota

func contextWithSession(ctx context.Context, record sessionRecord) context.Context {
	return context.WithValue(ctx, sessionContextKey, record)
}

func sessionFromContext(ctx context.Context) (sessionRecord, bool) {
	record, ok := ctx.Value(sessionContextKey).(sessionRecord)

	return record, ok
}

// Guard wraps next so that every request outside the exempt path set is
// authenticated. A request carrying an Authorization bearer token is
// authenticated as an API key and must hold the scope required for the path; a
// cookie request is authenticated as an admin session and must carry a matching
// CSRF token on unsafe methods. scopeOverrides maps a path to the scope it
// requires; paths without an override require admin:read for safe methods and
// admin:write otherwise.
func (s *Service) Guard(
	exempt []string,
	scopeOverrides map[string]Scope,
	next http.Handler,
) http.Handler {
	exemptSet := make(map[string]struct{}, len(exempt))
	for _, path := range exempt {
		exemptSet[path] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := exemptSet[r.URL.Path]; ok {
			next.ServeHTTP(w, r)

			return
		}
		if token, ok := bearerToken(r); ok {
			s.guardAPIKey(w, r, token, requiredScope(r.URL.Path, r.Method, scopeOverrides), next)

			return
		}
		s.guardSession(w, r, next)
	})
}

func (s *Service) guardSession(w http.ResponseWriter, r *http.Request, next http.Handler) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		s.unauthenticated(w, r)

		return
	}
	record, ok, err := s.sessions.lookup(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authentication failed")

		return
	}
	if !ok {
		s.unauthenticated(w, r)

		return
	}
	if !isSafeMethod(r.Method) && !validCSRFToken(r, record.CSRFToken) {
		writeError(w, http.StatusForbidden, "missing or invalid CSRF token")

		return
	}

	next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), record)))
}

// unauthenticated redirects a browser navigation to the login page and answers
// programmatic requests with a 401 so API clients still get a clear status.
func (s *Service) unauthenticated(w http.ResponseWriter, r *http.Request) {
	if isSafeMethod(r.Method) && acceptsHTML(r) {
		http.Redirect(w, r, PathLoginPage, http.StatusSeeOther)

		return
	}

	writeError(w, http.StatusUnauthorized, "authentication required")
}

// validCSRFToken accepts the session CSRF token from the X-CSRF-Token header or,
// for a form submission, from a csrf_token form field, so server-rendered HTML
// forms work without JavaScript.
func validCSRFToken(r *http.Request, want string) bool {
	if constantTimeMatch(r.Header.Get(csrfHeader), want) {
		return true
	}
	if strings.HasPrefix(r.Header.Get(contentType), formContentType) {
		return constantTimeMatch(r.PostFormValue(csrfFormField), want)
	}

	return false
}

func constantTimeMatch(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func acceptsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get(acceptHeader), htmlMediaType)
}

func (s *Service) guardAPIKey(
	w http.ResponseWriter,
	r *http.Request,
	token string,
	required Scope,
	next http.Handler,
) {
	id, _, ok := parseAPIKey(token)
	if !ok {
		s.observer.APIKeyRejected()
		writeError(w, http.StatusUnauthorized, "authentication required")

		return
	}
	if !s.keyLimiter.allow(id) {
		s.observer.APIKeyThrottled()
		writeError(w, http.StatusTooManyRequests, "too many requests, try again later")

		return
	}
	info, ok, err := s.apiKeys.authenticate(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authentication failed")

		return
	}
	if !ok {
		s.observer.APIKeyRejected()
		writeError(w, http.StatusUnauthorized, "authentication required")

		return
	}
	if !info.hasScope(required) {
		s.observer.APIKeyForbidden()
		writeError(w, http.StatusForbidden, "insufficient scope")

		return
	}

	next.ServeHTTP(w, r)
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get(authzHeader)
	if len(header) <= len(bearerScheme) ||
		!strings.EqualFold(header[:len(bearerScheme)], bearerScheme) {
		return "", false
	}
	token := strings.TrimSpace(header[len(bearerScheme):])
	if token == "" {
		return "", false
	}

	return token, true
}

func requiredScope(path, method string, overrides map[string]Scope) Scope {
	if scope, ok := overrides[path]; ok {
		return scope
	}
	if isSafeMethod(method) {
		return ScopeAdminRead
	}

	return ScopeAdminWrite
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
