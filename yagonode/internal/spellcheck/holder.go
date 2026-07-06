package spellcheck

import "sync/atomic"

// Holder publishes the current corrector for lock-free reads: a background sweep
// rebuilds the dictionary from the index and stores a fresh corrector while
// query handlers read whichever one is current.
type Holder struct {
	current atomic.Pointer[Corrector]
}

// NewHolder returns a holder whose current corrector is empty (corrects
// nothing) until the first Store.
func NewHolder() *Holder {
	holder := &Holder{}
	holder.current.Store(New(nil))

	return holder
}

// Current returns the corrector last stored; never nil.
func (h *Holder) Current() *Corrector {
	return h.current.Load()
}

// Store publishes a rebuilt corrector, replacing the previous one atomically.
func (h *Holder) Store(corrector *Corrector) {
	if corrector == nil {
		corrector = New(nil)
	}
	h.current.Store(corrector)
}
