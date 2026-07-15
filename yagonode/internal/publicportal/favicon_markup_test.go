package publicportal

import (
	"strings"
	"testing"
)

func TestPortalDeclaresSiteIcon(t *testing.T) {
	t.Parallel()

	_, body := get(t, New(&fakeSource{}, false), "/")
	if !strings.Contains(body, `<link rel="icon" type="image/svg+xml" href="/favicon.svg">`) {
		t.Fatal("portal does not declare the site icon")
	}
}
