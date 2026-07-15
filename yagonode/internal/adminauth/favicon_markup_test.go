package adminauth

import (
	"strings"
	"testing"
)

func TestStandaloneAuthPagesDeclareSiteIcon(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"templates/login.tmpl",
		"templates/setup.tmpl",
		"templates/restarting.tmpl",
	} {
		body, err := authTemplateFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(
			string(body),
			`<link rel="icon" type="image/svg+xml" href="/favicon.svg">`,
		) {
			t.Fatalf("%s does not declare the site icon", name)
		}
	}
}
