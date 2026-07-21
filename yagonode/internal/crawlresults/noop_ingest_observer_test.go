package crawlresults

type noopIngestObserver struct{}

func (noopIngestObserver) ObserveAbsorbed(int, int, int) {}

func (noopIngestObserver) ObserveDeferred() {}

func (noopIngestObserver) ObserveRejected() {}

func (noopIngestObserver) ObserveLowQuality() {}
