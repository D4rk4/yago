package pipeline

import (
	"errors"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

func pageOutcomeHTTPStatus(err error) uint32 {
	if statusError, ok := pagefetch.AsHTTPStatus(err); ok {
		return boundedHTTPStatus(statusError.Status)
	}
	var throttled *pagefetch.ThrottledError
	if errors.As(err, &throttled) {
		return boundedHTTPStatus(throttled.Status)
	}
	var gone *pagefetch.GoneError
	if errors.As(err, &gone) {
		return boundedHTTPStatus(gone.Status)
	}

	return 0
}

func boundedHTTPStatus(status int) uint32 {
	if status < 100 || status > 999 {
		return 0
	}

	return uint32(status)
}
