package tavilyapi

import (
	"net/http"
	"strconv"
	"time"
)

func enterSearchAdmission(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	admission SearchAdmission,
) (func(), bool) {
	release, status, retryAfter := admission(r)
	if status == 0 {
		return release, true
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter/time.Second))))
	code := "search_capacity_exceeded"
	message := "search capacity exceeded, try again later"
	if status == http.StatusTooManyRequests {
		code = "rate_limited"
		message = "too many requests, try again later"
	}
	writeError(w, status, code, message, id)

	return nil, false
}
