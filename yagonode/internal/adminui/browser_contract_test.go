package adminui

import (
	"net/url"
	"strings"
	"testing"
)

func TestAdminLayoutUsesStaticStrictCSPAssets(t *testing.T) {
	t.Parallel()

	console := New(Options{})
	page := do(t, console, "/admin/overview")
	for _, expected := range []string{
		`<link rel="icon" type="image/svg+xml" href="/favicon.svg">`,
		`<meta name="htmx-config" content='{"includeIndicatorStyles": false}'>`,
		`class="cds-icon-sprite"`,
	} {
		if !strings.Contains(page.body, expected) {
			t.Fatalf("admin layout missing %q", expected)
		}
	}
	if strings.Contains(page.body, `style="position:absolute"`) {
		t.Fatal("admin layout retains the inline SVG style")
	}

	stylesheet := do(t, console, "/admin/assets/carbon.css")
	for _, expected := range []string{
		`.cds-icon-sprite { position: absolute; overflow: hidden; }`,
		`.htmx-indicator { opacity: 0; }`,
		`.htmx-request.htmx-indicator { opacity: 1; transition: opacity 200ms ease-in; }`,
	} {
		if !strings.Contains(stylesheet.body, expected) {
			t.Fatalf("admin stylesheet missing %q", expected)
		}
	}
}

func TestRestartingPageKeepsStrictCSPWithoutInlineStyle(t *testing.T) {
	t.Parallel()

	console := New(Options{Restart: func() {}})
	page := doPost(t, console, "/admin/restart", url.Values{})
	if page.header.Get("Content-Security-Policy") != contentPol {
		t.Fatalf("content security policy = %q", page.header.Get("Content-Security-Policy"))
	}
	if strings.Contains(page.body, "<style") || strings.Contains(page.body, ` style=`) {
		t.Fatalf("restarting page contains inline style: %s", page.body)
	}
	for _, expected := range []string{
		`<link rel="stylesheet" href="/admin/assets/carbon.css">`,
		`class="cds-restarting-page"`,
		`class="cds-restarting-card"`,
	} {
		if !strings.Contains(page.body, expected) {
			t.Fatalf("restarting page missing %q", expected)
		}
	}
}

func TestPasswordFormDeclaresUsernameAssociation(t *testing.T) {
	t.Parallel()

	page := do(
		t,
		New(Options{Security: &fakeSecurity{view: securityViewWithKey()}}),
		"/admin/security",
	)
	for _, expected := range []string{
		`id="pw-username"`,
		`name="username"`,
		`autocomplete="username"`,
		`autocomplete="current-password"`,
		`autocomplete="new-password"`,
	} {
		if !strings.Contains(page.body, expected) {
			t.Fatalf("password form missing %q", expected)
		}
	}
}
