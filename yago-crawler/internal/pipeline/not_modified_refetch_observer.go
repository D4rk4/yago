package pipeline

type notModifiedRefetchObserver interface {
	NotModifiedRefetched()
}

func observeNotModifiedRefetch(observer Observer) {
	if refetches, ok := observer.(notModifiedRefetchObserver); ok {
		refetches.NotModifiedRefetched()
	}
}
