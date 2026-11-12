package searchcore

import "context"

type fullShardSearchContextKey struct{}

func WithFullShardSearch(ctx context.Context) context.Context {
	return context.WithValue(ctx, fullShardSearchContextKey{}, true)
}

func FullShardSearchRequired(ctx context.Context) bool {
	full, _ := ctx.Value(fullShardSearchContextKey{}).(bool)

	return full
}

func WithoutSearchProgressObserver(ctx context.Context) context.Context {
	return context.WithValue(ctx, searchProgressObserverKey{}, SearchProgressObserver(nil))
}
