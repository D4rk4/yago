package crawlseed

import "fmt"

type permanentExpansionError struct {
	cause error
}

func (e permanentExpansionError) Error() string {
	return e.cause.Error()
}

func (e permanentExpansionError) Unwrap() error {
	return e.cause
}

func (permanentExpansionError) Permanent() bool {
	return true
}

func permanentExpansionFailuref(format string, arguments ...any) error {
	return permanentExpansionError{cause: fmt.Errorf(format, arguments...)}
}

func markPermanentExpansionFailure(err error) error {
	return permanentExpansionError{cause: err}
}
