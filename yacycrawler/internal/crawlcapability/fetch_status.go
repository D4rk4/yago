package crawlcapability

type FetchStatus int

const (
	FetchSucceeded FetchStatus = iota
	FetchCeased
	FetchDeferred
	FetchNotAPage
	FetchTransient
)
