package pagefetch

import (
	"errors"
	"fmt"
)

type HTTPStatusError struct {
	Status int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("status %d: %v", e.Status, ErrPageRejected)
}

func (e *HTTPStatusError) Unwrap() error {
	return ErrPageRejected
}

func AsHTTPStatus(err error) (*HTTPStatusError, bool) {
	var statusError *HTTPStatusError
	if errors.As(err, &statusError) {
		return statusError, true
	}

	return nil, false
}
