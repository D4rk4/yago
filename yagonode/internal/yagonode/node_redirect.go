package yagonode

import (
	"net"
	"net/http"
	"strings"
)

// redirectHTTPS wraps a surface so that, when the operator has enabled the
// HTTP->HTTPS redirect, a plain-HTTP request is answered with a 308 to the
// https:// origin, preserving the path and query. TLS is expected to be
// terminated in front (a reverse proxy sets X-Forwarded-Proto). Loopback
// requests are never redirected, so the admin console reached over localhost
// cannot be pushed to an unreachable HTTPS origin.
func redirectHTTPS(toggles *runtimeToggles, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if toggles.HTTPSRedirectEnabled() && shouldRedirectToHTTPS(r) {
			writeHTTPSRedirect(w, r)

			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeHTTPSRedirect answers with a 308 to the https:// origin of the same
// request. The Location header is set directly rather than via http.Redirect
// because the target is the request's own host with only the scheme upgraded to
// https — it is a same-origin scheme upgrade, never a cross-origin destination.
func writeHTTPSRedirect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Location", "https://"+r.Host+r.URL.RequestURI())
	w.WriteHeader(http.StatusPermanentRedirect)
}

func shouldRedirectToHTTPS(r *http.Request) bool {
	if requestIsHTTPS(r) {
		return false
	}

	return !requestIsLoopback(r)
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func requestIsLoopback(r *http.Request) bool {
	host := r.Host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}
