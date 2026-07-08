package crawlcapability

import "time"

const (
	DisposalRefused              = "refused"
	DisposalNoIndex              = "noindex"
	DisposalUnsupportedMediaType = "unsupported-media-type"
	DisposalOversized            = "oversized"
	DisposalUnextractable        = "unextractable"
	DisposalFetchFailed          = "fetch-failed"
	DisposalBudgetTruncated      = "budget-truncated"
	DisposalContainerOverflow    = "container-overflow"
)

const (
	RefusalCeased   = "ceased"
	RefusalDeferred = "deferred"
)

type RunProgress interface {
	OrderReceived()
	OrderCompleted()
	OrderRedelivered()
	PageFetched()
	PagePublished(output string)
	PageDisposed(reason string)
	RefusalHonored(kind string)
	PublicationWaited()
	FetchObserved(elapsed time.Duration)
	BudgetExhausted()
}
