package tavilyapi

import "net/http"

// AuthenticatedRead reports whether the request carries credentials that pass
// read-scope authorization — the signal the public rate limiter uses to grant
// raised limits. Requests without any bearer token are not authenticated even
// when the policy would allow anonymous access.
func (p SearchAccessPolicy) AuthenticatedRead(r *http.Request) bool {
	if _, ok := bearerToken(r.Header.Get("Authorization")); !ok {
		return false
	}

	return p.authorize(r, ScopeRead) == DecisionAllow
}
