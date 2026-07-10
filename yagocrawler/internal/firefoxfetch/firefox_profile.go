package firefoxfetch

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// firefoxProfile is the throwaway profile launchFirefox writes before starting
// the browser: the Marionette port to bind, the egress-guarded proxy to route
// through, the crawl User-Agent to advertise, and whether the content sandbox
// stays on.
type firefoxProfile struct {
	MarionettePort int
	ProxyURL       string
	UserAgent      string
	Sandbox        bool
}

// writeFirefoxProfile creates a temporary profile directory whose user.js binds
// Marionette to the chosen port, routes every request through the crawler's
// egress-guarded forward proxy (the SSRF boundary), pins the crawl User-Agent,
// and silences Firefox's own background network chatter so the browser fetches
// only the page it was told to. The caller owns the returned directory and
// removes it when the session ends.
func writeFirefoxProfile(profile firefoxProfile) (string, error) {
	dir, err := os.MkdirTemp("", "yago-firefox-")
	if err != nil {
		return "", fmt.Errorf("create firefox profile: %w", err)
	}
	prefs, err := firefoxUserJS(profile)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "user.js"), []byte(prefs), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write firefox user.js: %w", err)
	}
	return dir, nil
}

func firefoxUserJS(profile firefoxProfile) (string, error) {
	var b strings.Builder
	pref := func(key string, value any) {
		switch v := value.(type) {
		case string:
			fmt.Fprintf(&b, "user_pref(%q, %q);\n", key, v)
		case bool:
			fmt.Fprintf(&b, "user_pref(%q, %t);\n", key, v)
		case int:
			fmt.Fprintf(&b, "user_pref(%q, %d);\n", key, v)
		}
	}

	pref("marionette.port", profile.MarionettePort)

	if profile.ProxyURL != "" {
		host, port, err := proxyHostPort(profile.ProxyURL)
		if err != nil {
			return "", err
		}
		pref("network.proxy.type", 1)
		pref("network.proxy.http", host)
		pref("network.proxy.http_port", port)
		pref("network.proxy.ssl", host)
		pref("network.proxy.ssl_port", port)
		pref("network.proxy.share_proxy_settings", true)
		// The egress guard must vet every request, including to loopback, so no
		// host is allowed to bypass the proxy.
		pref("network.proxy.no_proxies_on", "")
		pref("network.proxy.allow_hijacking_localhost", true)
	}
	if profile.UserAgent != "" {
		pref("general.useragent.override", profile.UserAgent)
	}
	if !profile.Sandbox {
		// Drop the content-process sandbox so Firefox starts on hosts that
		// restrict unprivileged user namespaces, matching MOZ_DISABLE_CONTENT_SANDBOX.
		pref("security.sandbox.content.level", 0)
	}

	// Keep Firefox from making its own background network calls: updates,
	// telemetry, safe-browsing list fetches, captive-portal probes, new-tab
	// content, and add-on metadata all reach the network otherwise and would
	// either bypass crawl intent or spam the egress guard.
	pref("app.update.enabled", false)
	pref("app.update.auto", false)
	pref("browser.shell.checkDefaultBrowser", false)
	pref("browser.startup.homepage", "about:blank")
	pref("browser.startup.page", 0)
	pref("startup.homepage_welcome_url", "about:blank")
	pref("startup.homepage_welcome_url.additional", "")
	pref("browser.newtabpage.enabled", false)
	pref("datareporting.healthreport.uploadEnabled", false)
	pref("datareporting.policy.dataSubmissionEnabled", false)
	pref("toolkit.telemetry.enabled", false)
	pref("toolkit.telemetry.unified", false)
	pref("toolkit.telemetry.archive.enabled", false)
	pref("browser.safebrowsing.malware.enabled", false)
	pref("browser.safebrowsing.phishing.enabled", false)
	pref("browser.safebrowsing.downloads.enabled", false)
	pref("network.captive-portal-service.enabled", false)
	pref("network.connectivity-service.enabled", false)
	pref("extensions.update.enabled", false)
	pref("extensions.getAddons.cache.enabled", false)
	// One content process is enough for a single serialized navigation and keeps
	// the process tree small on the crawler host.
	pref("fission.autostart", false)
	pref("dom.ipc.processCount", 1)
	pref("browser.cache.disk.enable", false)

	return b.String(), nil
}

func proxyHostPort(proxyURL string) (string, int, error) {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return "", 0, fmt.Errorf("parse browser proxy url %q: %w", proxyURL, err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return "", 0, fmt.Errorf("browser proxy url %q port: %w", proxyURL, err)
	}
	return parsed.Hostname(), port, nil
}
