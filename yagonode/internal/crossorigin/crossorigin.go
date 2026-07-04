// Package crossorigin applies a configurable, default-deny CORS policy to an
// HTTP handler. A request without an Origin header passes through untouched, so
// same-origin and server-to-server traffic is never affected; a cross-origin
// request receives access-control headers only when its origin is allowlisted.
package crossorigin

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	originHeader           = "Origin"
	requestMethodHeader    = "Access-Control-Request-Method"
	allowOriginHeader      = "Access-Control-Allow-Origin"
	allowCredentialsHeader = "Access-Control-Allow-Credentials"
	allowMethodsHeader     = "Access-Control-Allow-Methods"
	allowHeadersHeader     = "Access-Control-Allow-Headers"
	maxAgeHeader           = "Access-Control-Max-Age"
	varyHeader             = "Vary"
	wildcardOrigin         = "*"
)

// Config describes an origin allowlist and the preflight response it advertises.
type Config struct {
	AllowedOrigins   []string
	AllowCredentials bool
	AllowedMethods   []string
	AllowedHeaders   []string
	MaxAge           time.Duration
}

// Policy is an immutable, concurrency-safe CORS decision built from a Config.
type Policy struct {
	allowedOrigins   map[string]struct{}
	allowAnyOrigin   bool
	allowCredentials bool
	allowedMethods   string
	allowedHeaders   string
	maxAge           string
}

func NewPolicy(cfg Config) *Policy {
	policy := &Policy{
		allowedOrigins:   map[string]struct{}{},
		allowCredentials: cfg.AllowCredentials,
		allowedMethods:   strings.Join(cfg.AllowedMethods, ", "),
		allowedHeaders:   strings.Join(cfg.AllowedHeaders, ", "),
		maxAge:           strconv.Itoa(int(cfg.MaxAge.Seconds())),
	}
	for _, origin := range cfg.AllowedOrigins {
		if origin == wildcardOrigin {
			policy.allowAnyOrigin = true

			continue
		}
		policy.allowedOrigins[origin] = struct{}{}
	}

	return policy
}

// resolve reports the Access-Control-Allow-Origin value for origin and whether
// the origin is allowed. A credentialed wildcard echoes the request origin,
// since browsers reject a literal "*" alongside credentials.
func (p *Policy) resolve(origin string) (string, bool) {
	if _, ok := p.allowedOrigins[origin]; ok {
		return origin, true
	}
	if !p.allowAnyOrigin {
		return "", false
	}
	if p.allowCredentials {
		return origin, true
	}

	return wildcardOrigin, true
}

// Wrap returns next fronted by the policy. Preflight requests are answered here
// without reaching next; actual requests receive access-control headers and are
// forwarded.
func (p *Policy) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get(originHeader)
		if origin == "" {
			next.ServeHTTP(w, r)

			return
		}

		header := w.Header()
		header.Add(varyHeader, originHeader)
		allowOrigin, allowed := p.resolve(origin)
		if allowed {
			header.Set(allowOriginHeader, allowOrigin)
			if p.allowCredentials {
				header.Set(allowCredentialsHeader, "true")
			}
		}

		if isPreflight(r) {
			if allowed {
				header.Set(allowMethodsHeader, p.allowedMethods)
				header.Set(allowHeadersHeader, p.allowedHeaders)
				header.Set(maxAgeHeader, p.maxAge)
			}
			w.WriteHeader(http.StatusNoContent)

			return
		}

		next.ServeHTTP(w, r)
	})
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get(requestMethodHeader) != ""
}
