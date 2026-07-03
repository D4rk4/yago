//go:build e2e

package e2e

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

func dumpLogsOnFailure(t *testing.T, label string, container testcontainers.Container) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		reader, err := container.Logs(context.Background())
		if err != nil {
			t.Logf("%s logs unavailable: %v", label, err)
			return
		}
		defer func() { _ = reader.Close() }()

		file, err := os.CreateTemp("", "e2e-"+label+"-*.log")
		if err != nil {
			t.Logf("%s logs: create temp file failed: %v", label, err)
			return
		}
		defer func() { _ = file.Close() }()

		if _, err := io.Copy(file, reader); err != nil {
			t.Logf("%s logs: write to %s failed: %v", label, file.Name(), err)
			return
		}
		t.Logf("%s logs written to %s", label, file.Name())
	})
}
