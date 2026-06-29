package chromedpfetch

import "testing"

func proxyEnvironment(values map[string]string) func(string) string {
	return func(name string) string {
		return values[name]
	}
}

func TestProxyExecAllocatorOptionsEmptyWithoutEnvironment(t *testing.T) {
	options := proxyExecAllocatorOptions(proxyEnvironment(nil))
	if options != nil {
		t.Fatalf("options = %v, want nil", options)
	}
}

func TestProxyServerBuildsPerSchemeSpec(t *testing.T) {
	server := proxyServer("http://gateway:8080", "https://gateway:8443")
	if server != "http=http://gateway:8080;https=https://gateway:8443" {
		t.Errorf("server = %q", server)
	}
}

func TestProxyServerOmitsMissingScheme(t *testing.T) {
	if server := proxyServer("", "https://gateway:8443"); server != "https=https://gateway:8443" {
		t.Errorf("server = %q", server)
	}
}

func TestProxyBypassJoinsTrimmedHosts(t *testing.T) {
	if bypass := proxyBypass(" localhost , .internal ,"); bypass != "localhost;.internal" {
		t.Errorf("bypass = %q", bypass)
	}
}

func TestProxyEnvironmentValuePrefersFirstSet(t *testing.T) {
	environment := proxyEnvironment(map[string]string{"http_proxy": "http://lower:8080"})
	if value := proxyEnvironmentValue(
		environment,
		"HTTP_PROXY",
		"http_proxy",
	); value != "http://lower:8080" {
		t.Errorf("value = %q", value)
	}
}

func TestProxyExecAllocatorOptionsIncludesBypass(t *testing.T) {
	environment := proxyEnvironment(map[string]string{
		"HTTP_PROXY": "http://gateway:8080",
		"NO_PROXY":   "localhost",
	})
	if options := proxyExecAllocatorOptions(environment); len(options) != 2 {
		t.Fatalf("options = %d, want 2", len(options))
	}
}
