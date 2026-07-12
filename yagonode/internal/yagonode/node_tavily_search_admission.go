package yagonode

import (
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/publicratelimit"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
)

func newTavilySearchAdmission(limiter *publicratelimit.Limiter) tavilyapi.SearchAdmission {
	return func(r *http.Request) (func(), int, time.Duration) {
		if allowed, retryAfter := limiter.AllowRequest(r, true); !allowed {
			return nil, http.StatusTooManyRequests, retryAfter
		}
		release, admitted := publicratelimit.AdmitSearch()
		if !admitted {
			return nil, http.StatusServiceUnavailable, time.Second
		}

		return release, 0, 0
	}
}
