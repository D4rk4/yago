package websearch

const engineFetchConcurrency = 8

var processEngineFetchAdmission = newEngineFetchAdmission(engineFetchConcurrency)

type engineFetchAdmission struct {
	slots chan struct{}
}

func newEngineFetchAdmission(concurrency int) *engineFetchAdmission {
	return &engineFetchAdmission{slots: make(chan struct{}, concurrency)}
}

func (a *engineFetchAdmission) release() {
	<-a.slots
}
