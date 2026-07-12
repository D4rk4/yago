package documentstore

import (
	"context"
	"fmt"
)

func (d documentVault) enterStoredDocumentScan(
	ctx context.Context,
) (func(), error) {
	if d.scanAdmission == nil {
		return func() {}, nil
	}
	select {
	case d.scanAdmission <- struct{}{}:
		return func() { <-d.scanAdmission }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for stored document scan: %w", ctx.Err())
	}
}
