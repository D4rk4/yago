package httpguard

// IntakeGate bounds how many requests may occupy a guarded intake at once —
// the node-side stand-in for YaCy's system-load check on inbound DHT
// transfers (transferRWI.java rejects with "too high load") and its
// distributed-search DoS protection: Go has no portable load average, but the
// harm both guard against — unbounded concurrent intake work — is bounded
// directly by admission slots.
type IntakeGate struct {
	slots    chan struct{}
	onReject func()
}

// NewIntakeGate builds a gate admitting at most limit concurrent requests.
// A non-positive limit returns a nil gate, which admits everything.
func NewIntakeGate(limit int) *IntakeGate {
	return NewObservedIntakeGate(limit, nil)
}

// NewObservedIntakeGate builds a gate that also reports each shed request to
// onReject — the saturation signal of the USE method (OPS-07). A nil observer
// keeps the gate silent.
func NewObservedIntakeGate(limit int, onReject func()) *IntakeGate {
	if limit <= 0 {
		return nil
	}

	return &IntakeGate{slots: make(chan struct{}, limit), onReject: onReject}
}

// TryAcquire claims a slot without blocking; the caller must invoke release
// once the intake work is done. A nil gate admits everything.
func (g *IntakeGate) TryAcquire() (release func(), ok bool) {
	if g == nil {
		return func() {}, true
	}
	select {
	case g.slots <- struct{}{}:
		return func() { <-g.slots }, true
	default:
		if g.onReject != nil {
			g.onReject()
		}

		return nil, false
	}
}
