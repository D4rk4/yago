package cachedpage

const (
	maximumCachedPageURLBytes    = 8 << 10
	maximumConcurrentCachedPages = 8
)

type cachedPageAdmission struct {
	slots chan struct{}
}

func newCachedPageAdmission(limit int) *cachedPageAdmission {
	if limit <= 0 {
		return nil
	}

	return &cachedPageAdmission{slots: make(chan struct{}, limit)}
}

func (a *cachedPageAdmission) tryAcquire() (func(), bool) {
	if a == nil {
		return func() {}, true
	}
	select {
	case a.slots <- struct{}{}:
		return func() { <-a.slots }, true
	default:
		return nil, false
	}
}
