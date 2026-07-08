package crawlrun

import (
	"context"
	"fmt"
)

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return nil
}
