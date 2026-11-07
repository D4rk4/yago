package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentimport"
)

func TestMainReportsCommandSuccessAndFailure(t *testing.T) {
	input := filepath.Join(t.TempDir(), "fixture.jsonl")
	writeInterchangeFixture(t, input, []documentimport.Record{
		cliTestRecord("fixture", "https://fixture.example.test/"),
	})

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
		failStderr bool
	}{
		{
			name: "valid dry run",
			args: []string{
				"import", "--input", input, "--endpoint", "http://yago.example.test", "--dry-run",
			},
			wantCode: 0,
		},
		{name: "usage error", wantCode: 1, wantStderr: "usage: yacy-migrate"},
		{name: "stderr write failure", wantCode: 2, failStderr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oldArgs, oldErrors, oldExit := commandArgs, commandErrors, exitProcess
			t.Cleanup(func() {
				commandArgs, commandErrors, exitProcess = oldArgs, oldErrors, oldExit
			})
			commandArgs = func() []string { return test.args }
			var stderr bytes.Buffer
			if test.failStderr {
				commandErrors = errorWriter{}
			} else {
				commandErrors = &stderr
			}
			exitCode := -1
			exitProcess = func(code int) { exitCode = code }

			main()

			if exitCode != test.wantCode {
				t.Fatalf("exit code = %d, want %d", exitCode, test.wantCode)
			}
			if !strings.Contains(stderr.String(), test.wantStderr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), test.wantStderr)
			}
		})
	}
}

func TestCommandArgsReturnsArgumentsAfterExecutable(t *testing.T) {
	previous := os.Args
	os.Args = []string{"yacy-migrate", "export", "--dry-run"}
	t.Cleanup(func() { os.Args = previous })
	got := commandArgs()
	if len(got) != 2 || got[0] != "export" || got[1] != "--dry-run" {
		t.Fatalf("command arguments = %#v", got)
	}
}

func TestRunValidatesCommandsAndFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing command", want: "usage: yacy-migrate"},
		{name: "unknown command", args: []string{"crawl"}, want: `unknown command "crawl"`},
		{
			name: "invalid export flag",
			args: []string{"export", "--rows", "many"},
			want: "parse export options",
		},
		{
			name: "invalid import flag",
			args: []string{"import", "--unknown"},
			want: "parse import options",
		},
		{
			name: "invalid frontier-import flag",
			args: []string{"import-frontier", "--unknown"},
			want: "parse frontier import options",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := run(t.Context(), test.args, &output)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("run error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestRunFrontierImportDryRun(t *testing.T) {
	input, profiles := writeFrontierFixtures(t, []yaCyFrontierRecord{
		{
			Type: "crawlRequest", Version: 1, Stack: "LOCAL",
			URL: "https://example.test/one", ProfileHandle: "source-profile",
		},
	})
	var output bytes.Buffer
	err := run(t.Context(), []string{
		"import-frontier", "--input", input, "--profiles", profiles, "--dry-run",
	}, &output)
	if err != nil || !strings.Contains(output.String(), "dry-run: validated 1 queue records") {
		t.Fatalf("run frontier dry-run = %v, output=%q", err, output.String())
	}
}

func TestRunExportDryRunUsesConfiguredBasicAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "fixture-user" || password != "fixture-password" {
			t.Errorf("Solr basic auth = %q:%q, present=%t", user, password, ok)
		}
		if r.URL.Query().Get("rows") != "0" {
			t.Errorf("dry-run rows = %q, want 0", r.URL.Query().Get("rows"))
		}
		_, _ = io.WriteString(w, `{"response":{"numFound":1,"docs":[]},"nextCursorMark":"*"}`)
	}))
	defer server.Close()
	t.Setenv("YACY_USERNAME", "fixture-user")
	t.Setenv("YACY_PASSWORD", "fixture-password")
	var output bytes.Buffer
	err := run(t.Context(), []string{
		"export", "--solr-url", server.URL, "--source-id", "fixture", "--dry-run",
	}, &output)
	if err != nil {
		t.Fatalf("run export dry-run: %v", err)
	}
	if !strings.Contains(output.String(), "source reports 1 indexed rows") {
		t.Fatalf("export progress = %q", output.String())
	}
}

func closeMigrationFixture(t *testing.T, closer io.Closer) {
	t.Helper()
	t.Cleanup(func() {
		if err := closer.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Errorf("close migration test fixture: %v", err)
		}
	})
}
