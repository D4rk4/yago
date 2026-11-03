package yagonode

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseBinaryBuildDownloadsModulesThroughBoundedDownloader(t *testing.T) {
	contents, err := os.ReadFile("../../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}

	workflow := string(contents)
	download := strings.Index(workflow, "        run: sh deploy/download-go-modules.sh\n")
	build := strings.Index(workflow, "      - name: Build binaries\n")
	if download < 0 || build < 0 || download > build {
		t.Fatal(
			"release binary build does not download modules through the bounded downloader first",
		)
	}
}
