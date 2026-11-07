package searchcore

import "context"

type SearchProgress struct {
	Response        Response
	CompletedShards int
	SampledShards   int
	TotalShards     int
	Complete        bool
}

type SearchProgressObserver func(SearchProgress)

type searchProgressObserverKey struct{}

func WithSearchProgressObserver(
	ctx context.Context,
	observer SearchProgressObserver,
) context.Context {
	return context.WithValue(ctx, searchProgressObserverKey{}, observer)
}

func SearchProgressObserverFrom(ctx context.Context) SearchProgressObserver {
	observer, _ := ctx.Value(searchProgressObserverKey{}).(SearchProgressObserver)

	return observer
}

func ReportSearchProgress(ctx context.Context, progress SearchProgress) {
	if observer := SearchProgressObserverFrom(ctx); observer != nil {
		observer(progress)
	}
}
