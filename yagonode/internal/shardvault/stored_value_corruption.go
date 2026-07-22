package shardvault

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func storedValueCorruption(err error) error {
	return fmt.Errorf("%w: %w", vault.ErrCorruptValue, err)
}
