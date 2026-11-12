package searchindex

import "context"

func (b *BleveDiskIndex) WarmReadCache(ctx context.Context) error {
	b.readWarmupOnce.Do(func() {
		b.readWarmupError = b.prepareBleveReads(
			ctx,
			b.readCacheRoot,
			b.readCacheAdmission,
		)
	})

	return b.readWarmupError
}
