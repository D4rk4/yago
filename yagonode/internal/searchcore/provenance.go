package searchcore

// FromPeer reports whether the result was answered by another peer's index
// during the swarm fan-out.
func (r Result) FromPeer() bool { return r.Source == SourceRemote }

// FromWeb reports whether the result came from the external web-search fallback.
func (r Result) FromWeb() bool { return r.Source == SourceWeb }

// StoredLocally reports whether the result was served from this node's own
// stores. Local hits inside a global search carry the request's source
// (SourceGlobal), so this is the test surfaces must use for "do we hold this
// page" decisions like cached-copy links — not a SourceLocal comparison.
func (r Result) StoredLocally() bool { return !r.FromPeer() && !r.FromWeb() }
