package crawllease

import "context"

type leaseIdentityKey struct{}

func WithLeaseID(ctx context.Context, leaseID string) context.Context {
	return context.WithValue(ctx, leaseIdentityKey{}, leaseID)
}

func LeaseID(ctx context.Context) string {
	leaseID, _ := ctx.Value(leaseIdentityKey{}).(string)

	return leaseID
}
