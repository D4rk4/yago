package adminauth

import (
	"slices"
	"testing"
)

func TestParseScopesAcceptsKnownScopes(t *testing.T) {
	scopes, err := parseScopes([]string{
		string(ScopeAdminRead),
		string(ScopeAdminWrite),
		string(ScopeCrawlWrite),
		string(ScopeSearchRead),
		string(ScopeSearchRaw),
	})
	if err != nil {
		t.Fatalf("parseScopes: %v", err)
	}
	want := []Scope{
		ScopeAdminRead,
		ScopeAdminWrite,
		ScopeCrawlWrite,
		ScopeSearchRead,
		ScopeSearchRaw,
	}
	if !slices.Equal(scopes, want) {
		t.Fatalf("scopes = %v, want %v", scopes, want)
	}
}

func TestParseScopesCollapsesDuplicates(t *testing.T) {
	scopes, err := parseScopes([]string{
		string(ScopeAdminRead),
		string(ScopeAdminRead),
		string(ScopeCrawlWrite),
	})
	if err != nil {
		t.Fatalf("parseScopes: %v", err)
	}
	if !slices.Equal(scopes, []Scope{ScopeAdminRead, ScopeCrawlWrite}) {
		t.Fatalf("scopes = %v", scopes)
	}
}

func TestParseScopesRejectsUnknown(t *testing.T) {
	if _, err := parseScopes([]string{string(ScopeAdminRead), "storage:wipe"}); err == nil {
		t.Fatal("parseScopes accepted an unknown scope")
	}
}

func TestParseScopesRejectsEmpty(t *testing.T) {
	if _, err := parseScopes(nil); err == nil {
		t.Fatal("parseScopes accepted an empty scope list")
	}
}
