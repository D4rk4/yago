package yagonode

import "testing"

func TestNormalizePublicBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                                  "",
		"  https://search.example.org/  ":   "https://search.example.org",
		"http://proxy.example:8080/search/": "http://proxy.example:8080/search",
	}
	for raw, want := range cases {
		got, err := normalizePublicBaseURL(raw)
		if err != nil || got != want {
			t.Fatalf("normalize(%q) = %q %v, want %q", raw, got, err, want)
		}
	}
	for _, bad := range []string{
		"ftp://x.example", "not a url://", "/relative", "https://user@host.example",
	} {
		if _, err := normalizePublicBaseURL(bad); err == nil {
			t.Fatalf("normalize(%q) must fail", bad)
		}
	}
}

func TestPublicBaseURLToggle(t *testing.T) {
	toggles := newRuntimeToggles(nodeConfig{PublicBaseURL: "https://a.example"})
	if toggles.PublicBaseURL() != "https://a.example" {
		t.Fatalf("initial = %q", toggles.PublicBaseURL())
	}
	toggles.SetPublicBaseURL("https://b.example")
	if toggles.PublicBaseURL() != "https://b.example" {
		t.Fatalf("updated = %q", toggles.PublicBaseURL())
	}
	var nilToggles *runtimeToggles
	if nilToggles.PublicBaseURL() != "" {
		t.Fatal("nil toggles must read empty")
	}
	nilToggles.SetPublicBaseURL("x")
}

func TestLoadConfigRejectsBadPublicBaseURL(t *testing.T) {
	if _, err := loadDerivedConfigs(envWithBad(envPublicBaseURL, "ftp://x")); err == nil {
		t.Fatal("bad public base url must fail config load")
	}
}

func TestPublicBaseURLSettingDefinition(t *testing.T) {
	def, ok := indexSettingDefinitions()[settingKeyPublicBaseURL]
	if !ok {
		t.Fatal("public base url setting missing")
	}
	config := def.apply(nodeConfig{}, "https://s.example")
	if config.PublicBaseURL != "https://s.example" {
		t.Fatalf("apply = %q", config.PublicBaseURL)
	}
	if def.defaultValue(nodeConfig{PublicBaseURL: "https://d.example"}) != "https://d.example" {
		t.Fatal("default must read the config")
	}
	toggles := newRuntimeToggles(nodeConfig{})
	def.applyLive(toggles, "https://live.example")
	if toggles.PublicBaseURL() != "https://live.example" {
		t.Fatalf("live apply = %q", toggles.PublicBaseURL())
	}
	if def.restartRequired() {
		t.Fatal("public base url must apply live")
	}
}
