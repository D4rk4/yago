package services

import (
	"context"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
)

var ErrUnknownCountKind = fmt.Errorf("unknown count kind")

type Counter struct {
	rwi  ports.RWIStore
	urls ports.URLStore
}

func NewCounter(rwi ports.RWIStore, urls ports.URLStore) Counter {
	return Counter{rwi: rwi, urls: urls}
}

func (c Counter) Count(ctx context.Context, kind contracts.CountKind) (int, error) {
	switch kind {
	case contracts.RWICount:
		n, err := c.rwi.RWICount(ctx)

		return n, wrapCount(err)
	case contracts.RWIURLCount:
		n, err := c.rwi.ReferencedURLCount(ctx)

		return n, wrapCount(err)
	case contracts.LURLCount:
		n, err := c.urls.URLCount(ctx)

		return n, wrapCount(err)
	default:
		return 0, fmt.Errorf("%w: %d", ErrUnknownCountKind, kind)
	}
}

func wrapCount(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("count: %w", err)
}
