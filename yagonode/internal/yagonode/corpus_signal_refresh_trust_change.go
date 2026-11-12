package yagonode

import "context"

func (r *corpusSignalRefresh) afterHostTrustChange(
	ctx context.Context,
	changes <-chan struct{},
	open bool,
) <-chan struct{} {
	if !open {
		return nil
	}
	r.publishAuthority(ctx)

	return changes
}
