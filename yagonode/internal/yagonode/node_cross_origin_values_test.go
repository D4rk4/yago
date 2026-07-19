package yagonode

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestCrossOriginListCanonicalizesBoundedOrigins(t *testing.T) {
	raw := " HTTPS://Example.COM:443/, http://[2001:0db8::1]:80/, " +
		"https://bücher.example, https://example.com "
	origins, err := parseCrossOriginList(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"http://[2001:db8::1]",
		"https://example.com",
		"https://xn--bcher-kva.example",
	}
	if strings.Join(origins, ",") != strings.Join(want, ",") {
		t.Fatalf("origins = %q, want %q", origins, want)
	}

	wildcard, err := parseCrossOriginList("https://example.com,*,https://other.example")
	if err != nil || len(wildcard) != 1 || wildcard[0] != "*" {
		t.Fatalf("wildcard origins = %q, %v", wildcard, err)
	}
}

func TestCrossOriginListRejectsMalformedAndOversizedValues(t *testing.T) {
	invalid := []string{
		"null",
		"ftp://example.com",
		"https://user@example.com",
		"https://example.com/path",
		"https://example.com?query",
		"https://example.com#fragment",
		"https://example.com:",
		"https://example.com:0",
		"https://exa mple.com",
		"https://example.com\nhttps://other.example",
		"*,https://example.com/path",
		strings.Repeat("x", maximumCrossOriginConfigurationBytes+1),
	}
	for _, raw := range invalid {
		if _, err := parseCrossOriginList(raw); err == nil {
			t.Fatalf("origin list %q accepted", raw)
		}
	}

	many := make([]string, maximumCrossOrigins+1)
	for index := range many {
		many[index] = "https://origin-" + string(rune('a'+index%26)) +
			strings.Repeat("x", index/26) + ".example"
	}
	if _, err := parseCrossOriginList(strings.Join(many, ",")); err == nil {
		t.Fatal("oversized origin set accepted")
	}
}

func TestCrossOriginBootstrapAdminAndRuntimeShareCanonicalValues(t *testing.T) {
	raw := "HTTPS://UI.Example:443/"
	config, err := loadCrossOriginConfig(environmentValues{
		envAdminCORSOrigins:  raw,
		envSearchCORSOrigins: "*",
	}.get)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.AdminOrigins) != 1 || config.AdminOrigins[0] != "https://ui.example" ||
		len(config.SearchOrigins) != 1 || config.SearchOrigins[0] != "*" {
		t.Fatalf("bootstrap config = %+v", config)
	}

	definition := indexSettingDefinitions()["security.cors.admin"]
	normalized, err := definition.normalize(raw)
	if err != nil || normalized != "https://ui.example" {
		t.Fatalf("Admin normalization = %q, %v", normalized, err)
	}
	applied := definition.apply(nodeConfig{}, normalized)
	if len(applied.CrossOrigin.AdminOrigins) != 1 ||
		applied.CrossOrigin.AdminOrigins[0] != config.AdminOrigins[0] {
		t.Fatalf("Admin config = %+v", applied.CrossOrigin)
	}

	handler := wrapAdminCORS(
		config.AdminOrigins,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://node.example/",
		nil,
	)
	request.Header.Set("Origin", "https://ui.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "https://ui.example" {
		t.Fatalf("allow origin = %q", response.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCrossOriginBootstrapRejectsInvalidSearchOrigin(t *testing.T) {
	if _, err := loadCrossOriginConfig(environmentValues{
		envSearchCORSOrigins: "https://search.example/path",
	}.get); err == nil {
		t.Fatal("invalid search origin accepted")
	}
}

func TestRunRejectsInvalidCrossOriginConfigurationBeforeStorage(t *testing.T) {
	restoreMainSeams(t)
	setValidRunEnv(t)
	t.Setenv(envAdminCORSOrigins, "https://ui.example/path")
	opened := false
	openRuntimeVault = func(string, int64) (*vault.Vault, error) {
		opened = true

		return nil, errors.New("storage must not open")
	}

	err := run()
	if err == nil || !strings.Contains(err.Error(), envAdminCORSOrigins) {
		t.Fatalf("run error = %v", err)
	}
	if opened {
		t.Fatal("storage opened before CORS validation")
	}
}
