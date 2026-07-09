package main

import "testing"

func TestBuildExtractorDefaultRegistersAll(t *testing.T) {
	extractor, err := buildExtractor(ServiceConfig{MaxBodyBytes: 1 << 20})
	if err != nil {
		t.Fatalf("build extractor: %v", err)
	}
	if extractor == nil {
		t.Fatal("nil extractor")
	}
	// text/html routes to the html extractor.
	if _, err := extractor.Extract("http://h/p", "text/html",
		[]byte("<html><body></body></html>")); err == nil {
		t.Fatal("expected unextractable for empty html, dispatch reached extractor")
	}
}

func TestBuildExtractorAllowlistRestricts(t *testing.T) {
	extractor, err := buildExtractor(ServiceConfig{
		MaxBodyBytes: 1 << 20, ContentTypes: []string{"text/html"},
	})
	if err != nil {
		t.Fatalf("build extractor: %v", err)
	}
	if _, err := extractor.Extract("http://h/a.zip", "application/zip", []byte("x")); err == nil {
		t.Fatal("zip should be unsupported when allowlist excludes it")
	}
}

func TestBuildExtractorEmptyActiveSetErrors(t *testing.T) {
	if _, err := buildExtractor(ServiceConfig{
		MaxBodyBytes: 1 << 20, ContentTypes: []string{"application/unregistered"},
	}); err == nil {
		t.Fatal("allowlist matching nothing should error")
	}
}

func TestAllowedMediaTypes(t *testing.T) {
	if allowedMediaTypes(nil) != nil {
		t.Fatal("empty list should yield nil (all allowed)")
	}
	allow := allowedMediaTypes([]string{"text/html"})
	if !allow["text/html"] || allow["application/zip"] {
		t.Fatalf("unexpected allow set: %v", allow)
	}
}

func TestTraversalConfigMapsCaps(t *testing.T) {
	cfg := traversalConfig(ServiceConfig{RunPageBudget: 7, FrontierCap: 9})
	if cfg.RunPageBudget != 7 || cfg.FrontierCapacity != 9 {
		t.Fatalf("traversal config not mapped: %+v", cfg)
	}
	if cfg.FetchRetryLimit != fetchRetryLimit || cfg.MaxDeferralsPerURL != maxDeferPerURL {
		t.Fatal("traversal config constants not applied")
	}
}
