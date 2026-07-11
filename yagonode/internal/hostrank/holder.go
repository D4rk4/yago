package hostrank

import "sync/atomic"

// Holder serves the current host authority table atomically, so search requests
// read the live table without locking while a background refresh recomputes it.
type Holder struct {
	current atomic.Pointer[AuthorityTable]
}

// NewHolder returns a holder that serves an empty table until the first Store.
func NewHolder() *Holder {
	return &Holder{}
}

// Current returns the live authority table, or an empty (all-neutral) table
// before the first refresh has published one.
func (h *Holder) Current() AuthorityTable {
	if table := h.current.Load(); table != nil {
		return *table
	}

	return AuthorityTable{}
}

// Store atomically publishes a freshly computed authority table.
func (h *Holder) Store(table AuthorityTable) {
	h.current.Store(&table)
}
