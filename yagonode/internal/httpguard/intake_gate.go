package httpguard

// IntakeGate bounds how many requests may occupy a guarded intake at once —
// the node-side stand-in for YaCy's system-load check on inbound DHT
// transfers (transferRWI.java rejects with "too high load") and its
// distributed-search DoS protection: Go has no portable load average, but the
// harm both guard against — unbounded concurrent intake work — is bounded
// directly by admission slots.
type IntakeGate struct {
	slots chan struct{}
}

// NewIntakeGate builds a gate admitting at most limit concurrent requests.
// A non-positive limit returns a nil gate, which admits everything.
func NewIntakeGate(limit int) *IntakeGate {
	if limit <= 0 {
		return nil
	}

	return &IntakeGate{slots: make(chan struct{}, limit)}
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
		return nil, false
	}
}
