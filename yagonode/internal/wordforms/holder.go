package wordforms

import "sync/atomic"

// Holder publishes the current expander for lock-free reads: a background sweep
// rebuilds the stem groups from the index while query handlers read whichever
// expander is current.
type Holder struct {
	current atomic.Pointer[Expander]
}

// NewHolder returns a holder whose current expander is empty (expands nothing)
// until the first Store.
func NewHolder() *Holder {
	holder := &Holder{}
	holder.current.Store(New(nil, nil))

	return holder
}

// Current returns the expander last stored; never nil.
func (h *Holder) Current() *Expander {
	return h.current.Load()
}

// Store publishes a rebuilt expander, replacing the previous one atomically.
func (h *Holder) Store(expander *Expander) {
	if expander == nil {
		expander = New(nil, nil)
	}
	h.current.Store(expander)
}
