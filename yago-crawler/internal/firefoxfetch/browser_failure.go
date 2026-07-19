package firefoxfetch

import (
	"errors"
	"fmt"
)

type browserFailureError struct {
	reason BrowserFailureReason
	cause  error
}

func (e browserFailureError) Error() string {
	return fmt.Sprintf("browser %s failure: %v", e.reason, e.cause)
}

func (e browserFailureError) Unwrap() error {
	return e.cause
}

func browserFailureReason(err error) (BrowserFailureReason, bool) {
	var failure browserFailureError
	if !errors.As(err, &failure) {
		return "", false
	}

	return failure.reason, true
}
