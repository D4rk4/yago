package worker

import (
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type fetchAdmission struct {
	mutex   sync.Mutex
	maximum int
	active  int
}

func newFetchAdmission(maximum int) *fetchAdmission {
	return &fetchAdmission{maximum: maximum}
}

func (admission *fetchAdmission) TryAcquire() (func(), bool) {
	admission.mutex.Lock()
	if admission.active >= admission.maximum {
		admission.mutex.Unlock()
		return nil, false
	}
	admission.active++
	admission.mutex.Unlock()
	return func() {
		admission.mutex.Lock()
		admission.active--
		admission.mutex.Unlock()
	}, true
}

func (admission *fetchAdmission) SetMaximum(maximum uint32) bool {
	if maximum < 1 || maximum > yagocrawlcontract.MaximumFetchWorkerConcurrency {
		return false
	}
	admission.mutex.Lock()
	admission.maximum = int(maximum)
	admission.mutex.Unlock()
	return true
}
