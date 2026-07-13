package crawlseed

import (
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

func seedSourceStatusError(response *http.Response) error {
	if pagefetch.ThrottledStatus(response.StatusCode) {
		return &pagefetch.ThrottledError{
			Status: response.StatusCode,
			RetryAfter: pagefetch.ParseRetryAfter(
				response.Header.Get("Retry-After"),
				time.Now(),
			),
		}
	}
	if pagefetch.GoneStatus(response.StatusCode) {
		return markPermanentExpansionFailure(
			&pagefetch.GoneError{Status: response.StatusCode},
		)
	}
	if response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("status %d", response.StatusCode)
	}

	return permanentExpansionFailuref(
		"status %d: %w",
		response.StatusCode,
		pagefetch.ErrPageRejected,
	)
}
