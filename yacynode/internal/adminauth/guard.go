package adminauth

import (
	"context"
	"crypto/subtle"
	"net/http"
)

const csrfHeader = "X-CSRF-Token"

type contextKey int

const sessionContextKey contextKey = iota

func contextWithSession(ctx context.Context, record sessionRecord) context.Context {
	return context.WithValue(ctx, sessionContextKey, record)
}

func sessionFromContext(ctx context.Context) (sessionRecord, bool) {
	record, ok := ctx.Value(sessionContextKey).(sessionRecord)

	return record, ok
}

// Guard wraps next so that every request outside the exempt path set requires a
// valid admin session cookie, and every unsafe-method request additionally
// carries a matching CSRF token. The validated session is placed on the request
// context for downstream handlers.
func (s *Service) Guard(exempt []string, next http.Handler) http.Handler {
	exemptSet := make(map[string]struct{}, len(exempt))
	for _, path := range exempt {
		exemptSet[path] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := exemptSet[r.URL.Path]; ok {
			next.ServeHTTP(w, r)

			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")

			return
		}
		record, ok, err := s.sessions.lookup(r.Context(), cookie.Value)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "authentication failed")

			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")

			return
		}
		if !isSafeMethod(r.Method) &&
			subtle.ConstantTimeCompare(
				[]byte(r.Header.Get(csrfHeader)),
				[]byte(record.CSRFToken),
			) != 1 {
			writeError(w, http.StatusForbidden, "missing or invalid CSRF token")

			return
		}

		next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), record)))
	})
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
