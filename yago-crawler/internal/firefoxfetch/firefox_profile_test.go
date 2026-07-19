package firefoxfetch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirefoxUserJSBindsMarionetteProxyAndAgent(t *testing.T) {
	js, err := firefoxUserJS(firefoxProfile{
		MarionettePort: 2828,
		ProxyURL:       "http://127.0.0.1:4750",
		UserAgent:      "yago-crawler/1.0",
		Sandbox:        false,
		MaxRedirects:   7,
	})
	if err != nil {
		t.Fatalf("user.js: %v", err)
	}

	wants := []string{
		`user_pref("marionette.port", 2828);`,
		`user_pref("network.proxy.type", 1);`,
		`user_pref("network.proxy.http", "127.0.0.1");`,
		`user_pref("network.proxy.http_port", 4750);`,
		`user_pref("network.proxy.ssl", "127.0.0.1");`,
		`user_pref("network.proxy.ssl_port", 4750);`,
		`user_pref("network.proxy.no_proxies_on", "");`,
		`user_pref("general.useragent.override", "yago-crawler/1.0");`,
		`user_pref("network.http.redirection-limit", 7);`,
		// Sandbox off drops the content-process sandbox.
		`user_pref("security.sandbox.content.level", 0);`,
		// Background chatter silenced.
		`user_pref("toolkit.telemetry.enabled", false);`,
		`user_pref("app.update.enabled", false);`,
	}
	for _, want := range wants {
		if !strings.Contains(js, want) {
			t.Errorf("user.js missing pref:\n  %s", want)
		}
	}
}

func TestFirefoxUserJSClampsNegativeRedirectLimit(t *testing.T) {
	js, err := firefoxUserJS(firefoxProfile{MarionettePort: 2828, MaxRedirects: -1})
	if err != nil {
		t.Fatalf("user.js: %v", err)
	}
	if !strings.Contains(js, `user_pref("network.http.redirection-limit", 0);`) {
		t.Fatalf("user.js missing zero redirect limit:\n%s", js)
	}
}

func TestFirefoxUserJSKeepsSandboxWhenEnabled(t *testing.T) {
	js, err := firefoxUserJS(firefoxProfile{MarionettePort: 2828, Sandbox: true})
	if err != nil {
		t.Fatalf("user.js: %v", err)
	}
	if strings.Contains(js, "security.sandbox.content.level") {
		t.Error("the sandbox-disable pref must be absent when the sandbox is enabled")
	}
}

func TestFirefoxUserJSOmitsProxyPrefsWithoutURL(t *testing.T) {
	js, err := firefoxUserJS(firefoxProfile{MarionettePort: 2828})
	if err != nil {
		t.Fatalf("user.js: %v", err)
	}
	if strings.Contains(js, "network.proxy.type") {
		t.Error("no proxy prefs should be written without a proxy URL")
	}
}

func TestFirefoxUserJSRejectsProxyURLWithoutPort(t *testing.T) {
	if _, err := firefoxUserJS(firefoxProfile{
		MarionettePort: 2828,
		ProxyURL:       "http://no-port-here",
	}); err == nil {
		t.Fatal("expected an error for a proxy URL without a port")
	}
}

func TestWriteFirefoxProfileWritesUserJS(t *testing.T) {
	dir, err := writeFirefoxProfile(firefoxProfile{MarionettePort: 2828})
	if err != nil {
		t.Fatalf("write profile: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	//nolint:gosec // reads a file just written under a test temp dir
	body, err := os.ReadFile(filepath.Join(dir, "user.js"))
	if err != nil {
		t.Fatalf("read user.js: %v", err)
	}
	if !strings.Contains(string(body), `user_pref("marionette.port", 2828);`) {
		t.Errorf("user.js did not bind the marionette port:\n%s", body)
	}
}
