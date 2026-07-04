package yagonode

import (
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/crossorigin"
)

const (
	envAdminCORSOrigins  = "YAGO_ADMIN_CORS_ORIGINS"
	envSearchCORSOrigins = "YAGO_SEARCH_CORS_ORIGINS"
	corsPreflightMaxAge  = 10 * time.Minute
)

type crossOriginConfig struct {
	AdminOrigins  []string
	SearchOrigins []string
}

func loadCrossOriginConfig(getenv func(string) string) crossOriginConfig {
	return crossOriginConfig{
		AdminOrigins:  splitList(getenv(envAdminCORSOrigins)),
		SearchOrigins: splitList(getenv(envSearchCORSOrigins)),
	}
}

// wrapAdminCORS fronts the operations handler with a credentialed, default-deny
// CORS policy so a browser admin UI on an allowlisted origin can call the
// authenticated ops surface.
func wrapAdminCORS(origins []string, next http.Handler) http.Handler {
	return crossorigin.NewPolicy(crossorigin.Config{
		AllowedOrigins:   origins,
		AllowCredentials: true,
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders: []string{"Content-Type", "X-CSRF-Token", "Authorization"},
		MaxAge:         corsPreflightMaxAge,
	}).Wrap(next)
}

// wrapSearchCORS fronts the peer listener with an uncredentialed, default-deny
// CORS policy governing the public search endpoints. The /yacy/* peer protocol
// is server-to-server and sends no Origin header, so the policy never affects
// it.
func wrapSearchCORS(origins []string, next http.Handler) http.Handler {
	return crossorigin.NewPolicy(crossorigin.Config{
		AllowedOrigins:   origins,
		AllowCredentials: false,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		MaxAge:           corsPreflightMaxAge,
	}).Wrap(next)
}
