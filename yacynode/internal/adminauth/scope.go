package adminauth

import "fmt"

// Scope names a capability an API key may hold. A cookie session always holds
// every scope; API keys hold only the scopes granted at creation.
type Scope string

const (
	ScopeAdminRead  Scope = "admin:read"
	ScopeAdminWrite Scope = "admin:write"
	ScopeCrawlWrite Scope = "crawl:write"
	ScopeSearchRead Scope = "search:read"
	ScopeSearchRaw  Scope = "search:raw"
)

func knownScopes() map[Scope]struct{} {
	return map[Scope]struct{}{
		ScopeAdminRead:  {},
		ScopeAdminWrite: {},
		ScopeCrawlWrite: {},
		ScopeSearchRead: {},
		ScopeSearchRaw:  {},
	}
}

// parseScopes validates a requested scope list, rejecting an empty request or an
// unknown scope and collapsing duplicates while preserving first-seen order.
func parseScopes(requested []string) ([]Scope, error) {
	known := knownScopes()
	seen := make(map[Scope]struct{}, len(requested))
	scopes := make([]Scope, 0, len(requested))
	for _, raw := range requested {
		scope := Scope(raw)
		if _, ok := known[scope]; !ok {
			return nil, fmt.Errorf("unknown scope: %q", raw)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		scopes = append(scopes, scope)
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("at least one scope is required")
	}

	return scopes, nil
}
