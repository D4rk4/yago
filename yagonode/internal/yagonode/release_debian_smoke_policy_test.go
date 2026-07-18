package yagonode

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseDebianSmokeAcceptsPolicyExcludedDocumentation(t *testing.T) {
	contents, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}

	workflow := string(contents)
	start := strings.Index(workflow, "      - name: Smoke-test .deb across target distros (amd64)")
	end := strings.Index(workflow, "      - name: Install rpm tooling")
	if start < 0 || end <= start {
		t.Fatal("release workflow Debian smoke section is missing")
	}
	debianSmoke := workflow[start:end]

	for _, required := range []string{
		`dpkg-deb --fsys-tarfile "dist/yago_${version}_amd64.deb"`,
		`tar -xOf - ./usr/share/doc/yago/CJK_DICTIONARY_NOTICES.txt`,
		`cmp - yagonode/internal/searchindex/CJK_DICTIONARY_NOTICES.txt`,
		`"$img" sh -eux -c '`,
		`dpkg-query -L yago | grep -qx /usr/share/doc/yago/CJK_DICTIONARY_NOTICES.txt`,
	} {
		if !strings.Contains(debianSmoke, required) {
			t.Fatalf("release workflow Debian smoke missing %q", required)
		}
	}

	if strings.Contains(debianSmoke, "test -s /usr/share/doc/yago/CJK_DICTIONARY_NOTICES.txt") {
		t.Fatal("release workflow requires documentation bytes after a distro path exclusion")
	}
}
