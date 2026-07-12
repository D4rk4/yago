package tavilyapi

type requestAdmission struct {
	slots chan struct{}
}

func newRequestAdmission(limit int) *requestAdmission {
	return &requestAdmission{slots: make(chan struct{}, limit)}
}

func (a *requestAdmission) tryEnter() (func(), bool) {
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
