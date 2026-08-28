package searchindex

import (
	"context"
	"fmt"
	"runtime"
)

func newBleveDiskSearchReadAdmission() chan struct{} {
	return make(
		chan struct{},
		bleveDiskSearchReadParallelism(runtime.GOMAXPROCS(0)),
	)
}

func bleveDiskSearchReadParallelism(processors int) int {
	return max(1, processors)
}

func (b *BleveDiskIndex) admitSearchRead(
	ctx context.Context,
) (func(), error) {
	if b.searchReadAdmission == nil {
		return func() {}, nil
	}
	select {
	case b.searchReadAdmission <- struct{}{}:
		if cause := context.Cause(ctx); cause != nil {
			<-b.searchReadAdmission

			return nil, fmt.Errorf("search read admission: %w", cause)
		}

		return func() { <-b.searchReadAdmission }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("search read admission: %w", context.Cause(ctx))
	}
}
