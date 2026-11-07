package pagefetch

import (
	"errors"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type NotModifiedError struct {
	Validators yagocrawlcontract.HTTPValidators
}

func (e *NotModifiedError) Error() string {
	return "page not modified since the validators were issued"
}

func AsNotModified(err error) (*NotModifiedError, bool) {
	var notModified *NotModifiedError
	if errors.As(err, &notModified) {
		return notModified, true
	}

	return nil, false
}
