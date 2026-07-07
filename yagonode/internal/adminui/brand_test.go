package adminui

import (
	"strings"
	"testing"
)

func TestBrandWordmarkLinksToRepo(t *testing.T) {
	t.Parallel()

	console := New(Options{Config: fakeConfig{view: ConfigView{}}})
	got := do(t, console, "/admin/configuration")
	for _, want := range []string{
		`href="https://github.com/D4rk4/yago"`,
		`target="_blank"`,
		`rel="noopener noreferrer"`,
		`<span class="cds-brand__ya">ya</span>`,
		`<span class="cds-brand__go">go</span>`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("brand wordmark missing %q", want)
		}
	}
}
