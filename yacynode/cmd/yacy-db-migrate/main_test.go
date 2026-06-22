package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMigratesAndReportsIdempotently(t *testing.T) {
	path, _ := seedLegacyDB(t)

	var first bytes.Buffer
	if err := run(path, &first); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if !strings.Contains(first.String(), "migrated to vault schema") {
		t.Errorf("first run output = %q", first.String())
	}

	var second bytes.Buffer
	if err := run(path, &second); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !strings.Contains(second.String(), "already on vault schema") {
		t.Errorf("second run output = %q", second.String())
	}
}

func TestRunRequiresPath(t *testing.T) {
	if err := run("", &bytes.Buffer{}); !errors.Is(err, errMissingDBPath) {
		t.Fatalf("run(\"\") error = %v, want errMissingDBPath", err)
	}
}

func TestRunRejectsUnopenableDatabase(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "absent", "node.db")
	if err := run(missingDir, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error opening database under missing directory")
	}
}
