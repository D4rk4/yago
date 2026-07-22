package vault

import (
	"errors"
	"fmt"
)

var ErrCorruptValue = errors.New("corrupt stored value")

func corruptValueDecodeError(name Name, err error) error {
	return fmt.Errorf("%w: decode %s: %w", ErrCorruptValue, name, err)
}
