package searchcore

// FirstSeenBounded reports whether the request bounds the time this node first
// saw a document. Either half of the window is enough on its own: a start alone
// asks for pages discovered since then, an end alone for pages discovered up to
// then.
//
// The predicate decides more than filtering. First-seen is a property of this
// node's own index — peers neither send nor receive it, and an external web
// provider knows nothing of it — so a row this node does not hold cannot be
// shown to fall inside the window and drops. Every stage that would fetch such
// a row therefore reads this to decide whether that fetch can pay for itself.
func (r Request) FirstSeenBounded() bool {
	return !r.MinFirstSeen.IsZero() || !r.MaxFirstSeen.IsZero()
}
