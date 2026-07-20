package yagonode

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminauth"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
)

func TestBuildSearchScopeAuthorizer(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	service, err := adminauth.New(storage, adminauth.Config{})
	if err != nil {
		t.Fatalf("adminauth.New: %v", err)
	}
	if buildSearchScopeAuthorizer(service) == nil {
		t.Fatal("authorizer must be built")
	}
}

func TestLegacySearchAPIKeyPolicy(t *testing.T) {
	config := nodeConfig{SearchAPIKey: "static"}
	if got := legacySearchAPIKeyFor(config); got != "static" {
		t.Fatalf("legacy token = %q", got)
	}
	config.SearchRequireAPIKey = true
	if got := legacySearchAPIKeyFor(config); got != "" {
		t.Fatalf("scoped-only legacy token = %q, want empty", got)
	}
}

func TestTavilyDecisionMapping(t *testing.T) {
	for outcome, want := range map[adminauth.APIKeyOutcome]tavilyapi.AuthDecision{
		adminauth.APIKeyAuthorized:      tavilyapi.DecisionAllow,
		adminauth.APIKeyThrottled:       tavilyapi.DecisionThrottled,
		adminauth.APIKeyForbidden:       tavilyapi.DecisionForbidden,
		adminauth.APIKeyUnavailable:     tavilyapi.DecisionUnavailable,
		adminauth.APIKeyUnauthenticated: tavilyapi.DecisionUnauthenticated,
	} {
		if got := tavilyDecision(outcome); got != want {
			t.Errorf("tavilyDecision(%v) = %v, want %v", outcome, got, want)
		}
	}
}

func TestAdminSearchScopeMapping(t *testing.T) {
	if got := adminSearchScope(tavilyapi.ScopeRaw); got != adminauth.ScopeSearchRaw {
		t.Errorf("raw -> %v", got)
	}
	if got := adminSearchScope(tavilyapi.ScopeRead); got != adminauth.ScopeSearchRead {
		t.Errorf("read -> %v", got)
	}
}

func TestSearchAccessPolicyCombinesAuthorizerAndStaticToken(t *testing.T) {
	scoped := searchAccessPolicy(publicSearchAssembly{
		searchAuthorizer: searchScopeAuthorizer{},
		searchAPIKey:     "static",
	})
	if scoped.Authorizer == nil || scoped.BearerToken != "static" {
		t.Fatalf("scoped policy = %#v", scoped)
	}

	static := searchAccessPolicy(publicSearchAssembly{searchAPIKey: "static"})
	if static.Authorizer != nil || static.BearerToken != "static" {
		t.Fatalf("static policy = %#v", static)
	}
}

func TestLoadDerivedConfigsRequireAPIKey(t *testing.T) {
	getenv := func(key string) string {
		if key == envSearchRequireAPIKey {
			return "true"
		}

		return ""
	}
	derived, err := loadDerivedConfigs(getenv)
	if err != nil {
		t.Fatalf("loadDerivedConfigs: %v", err)
	}
	if !derived.requireAPIKey {
		t.Fatal("YAGO_SEARCH_REQUIRE_API_KEY=true should disable the legacy token")
	}
}

func TestScopedOnlySettingExplainsLegacyCompatibility(t *testing.T) {
	definition := settingByKey(
		t,
		storageAndAccessDefinitions(),
		"search.api.scoped_access",
	)
	if definition.title != "Require scoped API keys only" {
		t.Fatalf("title = %q", definition.title)
	}
	for _, phrase := range []string{"holding the required scope", "legacy static"} {
		if !strings.Contains(definition.description, phrase) {
			t.Fatalf("description missing %q: %q", phrase, definition.description)
		}
	}
}
