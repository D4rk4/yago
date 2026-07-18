package vault

import (
	"context"
	"fmt"
)

type bucketPresenceEngine interface {
	BucketProvisioned(context.Context, Name) (bool, error)
}

func (v *Vault) bucketProvisioned(ctx context.Context, name Name) (bool, error) {
	lease, err := v.acquireEngineLease()
	if err != nil {
		return false, err
	}
	defer lease.release()
	presence, ok := lease.engine.(bucketPresenceEngine)
	if !ok {
		return false, fmt.Errorf("storage engine does not report bucket presence")
	}

	present, err := presence.BucketProvisioned(ctx, name)
	if err != nil {
		return false, fmt.Errorf("inspect bucket %s presence: %w", name, err)
	}

	return present, nil
}
