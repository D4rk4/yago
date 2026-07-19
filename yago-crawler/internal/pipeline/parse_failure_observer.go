package pipeline

type parseFailureObserver interface {
	ParseFailed()
}

func observeParseFailure(observer Observer) {
	if parseFailures, ok := observer.(parseFailureObserver); ok {
		parseFailures.ParseFailed()
	}
}
