package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateExportOptionsRejectsUnsafeOrIncompleteInputs(t *testing.T) {
	valid := exportOptions{
		SolrURL: "https://solr.example.test/solr/select?core=collection1",
		Output:  filepath.Join(t.TempDir(), "export.jsonl"), SourceID: "fixture", Rows: 16,
	}
	for _, test := range []struct {
		name string
		edit func(*exportOptions)
	}{
		{name: "malformed URL", edit: func(o *exportOptions) { o.SolrURL = "http://[::1" }},
		{name: "relative URL", edit: func(o *exportOptions) { o.SolrURL = "/solr/select" }},
		{name: "unsupported scheme", edit: func(o *exportOptions) { o.SolrURL = "ftp://solr.example.test/select" }},
		{name: "embedded credentials", edit: func(o *exportOptions) { o.SolrURL = "https://user:secret@solr.example.test/select" }},
		{name: "fragment", edit: func(o *exportOptions) { o.SolrURL += "#fragment" }},
		{name: "blank source ID", edit: func(o *exportOptions) { o.SourceID = " " }},
		{name: "oversized source ID", edit: func(o *exportOptions) { o.SourceID = strings.Repeat("x", 129) }},
		{name: "zero rows", edit: func(o *exportOptions) { o.Rows = 0 }},
		{name: "too many rows", edit: func(o *exportOptions) { o.Rows = maximumSolrRows + 1 }},
		{name: "missing output", edit: func(o *exportOptions) { o.Output = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.edit(&options)
			if _, err := validateExportOptions(options); err == nil {
				t.Fatal("invalid export options were accepted")
			}
		})
	}
	dryRun := valid
	dryRun.Output, dryRun.DryRun = "", true
	if _, err := validateExportOptions(dryRun); err != nil {
		t.Fatalf("dry-run without an output path rejected: %v", err)
	}
	if _, err := validateExportOptions(valid); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
}

func TestExportCheckpointValidationAndFileSafety(t *testing.T) {
	endpoint, _ := url.Parse("https://solr.example.test/select")
	options := exportOptions{
		Output:   filepath.Join(t.TempDir(), "corpus.jsonl"),
		SourceID: "fixture",
	}
	state := exportCheckpoint{
		SourceID:          options.SourceID,
		SourceFingerprint: endpointFingerprint(endpoint),
	}
	if err := validateExportCheckpoint(state, true, options, endpoint); err != nil {
		t.Fatalf("valid checkpoint rejected: %v", err)
	}
	if err := validateExportCheckpoint(state, false, options, endpoint); err != nil {
		t.Fatalf("missing checkpoint rejected: %v", err)
	}
	wrongSource := options
	wrongSource.SourceID = "other"
	if err := validateExportCheckpoint(state, true, wrongSource, endpoint); err == nil {
		t.Fatal("checkpoint for another source ID was accepted")
	}
	otherEndpoint, _ := url.Parse("https://other.example.test/select")
	if err := validateExportCheckpoint(state, true, options, otherEndpoint); err == nil {
		t.Fatal("checkpoint for another Solr endpoint was accepted")
	}

	output := options.Output
	if err := os.WriteFile(output, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openExportFile(
		output,
		false,
		0,
	); err == nil ||
		!strings.Contains(err.Error(), "already exists without a checkpoint") {
		t.Fatalf("existing output without state error = %v", err)
	}
	if _, err := openExportFile(
		filepath.Join(t.TempDir(), "missing", "out.jsonl"),
		false,
		0,
	); err == nil ||
		!strings.Contains(err.Error(), "open export output") {
		t.Fatalf("unwritable output error = %v", err)
	}
	if _, err := openExportFile(
		"\x00",
		false,
		0,
	); err == nil ||
		!strings.Contains(err.Error(), "inspect output path") {
		t.Fatalf("invalid output path stat error = %v", err)
	}
	if _, err := openExportFile(
		output,
		true,
		int64(len("existing")+1),
	); err == nil ||
		!strings.Contains(err.Error(), "outside the output file") {
		t.Fatalf("out-of-range checkpoint offset error = %v", err)
	}
	if _, err := openExportFile(
		output,
		true,
		-1,
	); err == nil ||
		!strings.Contains(err.Error(), "outside the output file") {
		t.Fatalf("negative checkpoint offset error = %v", err)
	}
	file, err := openExportFile(output, true, 3)
	if err != nil {
		t.Fatalf("resume output: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() != 3 {
		t.Fatalf("resumed output size = %v, err %v", info, err)
	}
}

func TestStartExportHandlesCheckpointAndCompletedState(t *testing.T) {
	endpoint, _ := url.Parse("https://solr.example.test/select")
	output := filepath.Join(t.TempDir(), "out.jsonl")
	options := exportOptions{
		SolrURL:  endpoint.String(),
		Output:   output,
		SourceID: "fixture",
		Rows:     1,
	}
	statePath := output + ".state.json"
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := startExport(options, endpoint, io.Discard); err == nil {
		t.Fatal("checkpoint read error was ignored")
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := startExport(options, endpoint, io.Discard); err == nil {
		t.Fatal("invalid checkpoint was accepted")
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	complete := exportCheckpoint{
		Version:  1,
		SourceID: options.SourceID,
		SourceFingerprint: endpointFingerprint(
			endpoint,
		),
		Cursor:   "done",
		Complete: true,
		Records:  9,
	}
	if err := saveCheckpoint(statePath, complete); err != nil {
		t.Fatal(err)
	}
	if _, done, err := startExport(options, endpoint, errorWriter{}); err == nil || done {
		t.Fatalf("complete progress failure: done=%t err=%v", done, err)
	}
	var progress strings.Builder
	session, done, err := startExport(options, endpoint, &progress)
	if err != nil || !done || session != nil || !strings.Contains(progress.String(), "9 records") {
		t.Fatalf(
			"completed export = session %v, done %t, err %v, progress %q",
			session,
			done,
			err,
			progress.String(),
		)
	}
	if err := exportCorpus(t.Context(), options, io.Discard); err != nil {
		t.Fatalf("exportCorpus should accept the completed checkpoint: %v", err)
	}
	invalid := options
	invalid.SolrURL = "relative"
	if err := exportCorpus(t.Context(), invalid, io.Discard); err == nil {
		t.Fatal("exportCorpus accepted an invalid endpoint")
	}
	missingOutput := options
	missingOutput.Output = filepath.Join(t.TempDir(), "missing", "export.jsonl")
	if _, _, err := startExport(
		missingOutput,
		endpoint,
		io.Discard,
	); err == nil ||
		!strings.Contains(err.Error(), "open export output") {
		t.Fatalf("startExport output-open error = %v", err)
	}
}

func TestRunExportSessionRejectsMissingSession(t *testing.T) {
	endpoint, _ := url.Parse("https://solr.example.test/select")
	if err := runExportSession(
		t.Context(),
		nil,
		exportOptions{SourceID: "fixture", Rows: 1},
		newSolrReader(endpoint, exportOptions{}),
		io.Discard,
	); err == nil || !strings.Contains(err.Error(), "export session was not initialized") {
		t.Fatalf("nil export session error = %v", err)
	}
}

func TestExportCheckpointPersistenceRejectsInvalidPaths(t *testing.T) {
	if _, _, err := loadExportCheckpoint(t.TempDir()); err == nil {
		t.Fatal("checkpoint directory read failure was ignored")
	}
	if err := saveCheckpoint(
		filepath.Join(t.TempDir(), "missing", "state.json"),
		exportCheckpoint{Version: 1},
	); err == nil {
		t.Fatal("checkpoint temp creation failure was ignored")
	}
	if err := saveCheckpoint(t.TempDir(), exportCheckpoint{Version: 1}); err == nil ||
		!strings.Contains(err.Error(), "replace checkpoint") {
		t.Fatalf("checkpoint replace failure = %v", err)
	}
	if err := saveCheckpoint(filepath.Join(t.TempDir(), "state.json"), func() {}); err == nil ||
		!strings.Contains(err.Error(), "encode checkpoint") {
		t.Fatalf("checkpoint marshal failure = %v", err)
	}
	if err := syncCheckpointDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing checkpoint directory was accepted")
	}
}

func TestExportCheckpointTemporaryWriteAndCleanupFailures(t *testing.T) {
	writeFailure := newControlledExportFile(t)
	writeFailure.writeErr = errors.New("checkpoint write failed")
	if err := persistCheckpointTemporary(
		writeFailure,
		writeFailure.Name(),
		filepath.Join(t.TempDir(), "state.json"),
		filepath.Dir(writeFailure.Name()),
		[]byte("state"),
	); err == nil || !strings.Contains(err.Error(), "write checkpoint") {
		t.Fatalf("checkpoint temporary write failure = %v", err)
	}
	cleanupFailure := newControlledExportFile(t)
	cleanupDir := filepath.Join(t.TempDir(), "not-a-temp-file")
	if err := os.Mkdir(cleanupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(cleanupDir, "keep"),
		[]byte("fixture"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	cleanupFailure.chmodErr = errors.New("checkpoint chmod failed")
	if err := persistCheckpointTemporary(
		cleanupFailure,
		cleanupDir,
		filepath.Join(t.TempDir(), "state.json"),
		filepath.Dir(cleanupDir),
		[]byte("state"),
	); err == nil || !strings.Contains(err.Error(), "remove checkpoint temporary file") {
		t.Fatalf("temporary cleanup failure = %v", err)
	}
}

func TestExportCheckpointFilesystemHelpers(t *testing.T) {
	if err := syncCheckpointDirectory(t.TempDir()); err != nil {
		t.Fatalf("sync checkpoint directory: %v", err)
	}
	if err := removeCheckpointTemporary(filepath.Join(t.TempDir(), "already-removed")); err != nil {
		t.Fatalf("missing temporary checkpoint cleanup: %v", err)
	}
	nonEmptyDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(nonEmptyDir, "keep"),
		[]byte("fixture"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := removeCheckpointTemporary(nonEmptyDir); err == nil {
		t.Fatal("failed temporary checkpoint cleanup was ignored")
	}
	if wrapCheckpointError("sync", nil) != nil ||
		wrapCheckpointError("sync", errors.New("failure")) == nil {
		t.Fatal("checkpoint error wrapper did not preserve nil and non-nil errors")
	}
	closed, err := os.CreateTemp(t.TempDir(), "closed-output-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := closeFileAfterError(
		closed,
		"test operation",
		errors.New("primary failure"),
	); !strings.Contains(
		err.Error(),
		"test operation",
	) {
		t.Fatalf("close-on-error result = %v", err)
	}
	if err := writeCheckpointTemporary(closed, []byte("state")); err == nil ||
		!strings.Contains(err.Error(), "restrict checkpoint permissions") {
		t.Fatalf("closed checkpoint file error = %v", err)
	}
	if err := writeCheckpointTemporary(newControlledExportFile(t), []byte("state")); err != nil {
		t.Fatalf("checkpoint temporary write: %v", err)
	}
}

func TestWriteCheckpointTemporaryReportsOperationFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		file *controlledExportFile
		want string
	}{
		{name: "chmod", file: newControlledExportFile(t), want: "restrict checkpoint permissions"},
		{name: "write", file: newControlledExportFile(t), want: "write checkpoint"},
		{name: "sync", file: newControlledExportFile(t), want: "sync checkpoint"},
	} {
		t.Run(test.name, func(t *testing.T) {
			switch test.name {
			case "chmod":
				test.file.chmodErr = errors.New("chmod failed")
			case "write":
				test.file.writeErr = errors.New("write failed")
			case "sync":
				test.file.syncErr = errors.New("sync failed")
			}
			if err := writeCheckpointTemporary(test.file, []byte("state")); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("checkpoint temporary error = %v, want %q", err, test.want)
			}
		})
	}
	closeErrorFile := newControlledExportFile(t)
	closeErrorFile.closeErr = errors.New("close failed")
	if err := writeCheckpointTemporary(closeErrorFile, []byte("state")); err == nil ||
		!strings.Contains(err.Error(), "close checkpoint temporary file") {
		t.Fatalf("checkpoint close error = %v", err)
	}
}

func TestSyncCheckpointDirectoryReportsSyncAndCloseFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		dir  *controlledExportFile
	}{
		{name: "sync", dir: newControlledExportFile(t)},
		{name: "close", dir: newControlledExportFile(t)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "sync" {
				test.dir.syncErr = errors.New("directory sync failed")
			} else {
				test.dir.closeErr = errors.New("directory close failed")
			}
			if err := syncAndCloseCheckpointDirectory(test.dir); err == nil {
				t.Fatal("checkpoint directory failure was ignored")
			}
		})
	}
}

func TestExportOutputPreparationReportsFileOperationFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*controlledExportFile)
		want string
	}{
		{name: "chmod", edit: func(file *controlledExportFile) { file.chmodErr = errors.New("chmod") }, want: "restrict export output permissions"},
		{name: "stat", edit: func(file *controlledExportFile) { file.statErr = errors.New("stat") }, want: "inspect export output"},
		{name: "truncate", edit: func(file *controlledExportFile) { file.truncateErr = errors.New("truncate") }, want: "truncate export output"},
		{name: "seek", edit: func(file *controlledExportFile) { file.seekErr = errors.New("seek") }, want: "seek export output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := newControlledExportFile(t)
			_, _ = file.Write([]byte("existing"))
			test.edit(file)
			if err := prepareExportFile(
				file,
				2,
			); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("prepare export file error = %v, want %q", err, test.want)
			}
		})
	}
	file := newControlledExportFile(t)
	if err := closeFileAfterError(
		file,
		"forced preparation",
		errors.New("primary"),
	); !strings.Contains(
		err.Error(),
		"forced preparation",
	) {
		t.Fatalf("close-on-error result = %v", err)
	}
	closeFailureFile := newControlledExportFile(t)
	closeFailureFile.closeErr = errors.New("close failed")
	if err := closeFileAfterError(
		closeFailureFile,
		"forced preparation",
		errors.New("primary"),
	); !strings.Contains(
		err.Error(),
		"close file after forced preparation",
	) {
		t.Fatalf("close failure was not joined: %v", err)
	}
}

func TestExportHeaderWriteAndSyncFailures(t *testing.T) {
	tests := []struct {
		name       string
		writerSize int
		file       func(*testing.T) *controlledExportFile
		statePath  func(*testing.T) string
		want       string
	}{
		{
			name:       "write",
			writerSize: 1,
			file:       newControlledExportFile,
			want:       "write interchange record",
		},
		{
			name:       "flush",
			writerSize: 4096,
			file:       newControlledExportFile,
			want:       "flush export header",
		},
		{name: "sync", writerSize: 4096, file: func(t *testing.T) *controlledExportFile {
			f := newControlledExportFile(t)
			f.syncErr = errors.New("sync")
			return f
		}, want: "sync export header"},
		{name: "seek", writerSize: 4096, file: func(t *testing.T) *controlledExportFile {
			f := newControlledExportFile(t)
			f.seekErr = errors.New("seek")
			return f
		}, want: "measure export header offset"},
		{
			name:       "checkpoint",
			writerSize: 4096,
			file:       newControlledExportFile,
			statePath:  func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing", "state.json") },
			want:       "create checkpoint temporary file",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := test.file(t)
			statePath := filepath.Join(t.TempDir(), "state.json")
			if test.statePath != nil {
				statePath = test.statePath(t)
			}
			var writerTarget io.Writer = file
			if test.name == "write" || test.name == "flush" {
				writerTarget = errorWriter{}
			}
			writer := bufio.NewWriterSize(writerTarget, test.writerSize)
			session := &exportSession{
				output:            file,
				writer:            writer,
				statePath:         statePath,
				sourceFingerprint: "fixture",
			}
			if err := session.writeHeader(
				"fixture",
			); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("write header error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestExportHeaderCloseAndCheckpointFailures(t *testing.T) {
	closeFailure := newControlledExportFile(t)
	closeFailure.closeErr = errors.New("output close failed")
	if err := closeExportOutput(
		closeFailure,
		errors.New("prior failure"),
	); !strings.Contains(
		err.Error(),
		"close export output",
	) {
		t.Fatalf("export close error = %v", err)
	}
	validOutput := newControlledExportFile(t)
	validOptions := exportOptions{SourceID: "fixture", Rows: 1}
	endpoint, _ := url.Parse("https://solr.example.test/select")
	if err := runExportSession(t.Context(), &exportSession{
		output: validOutput,
		writer: bufio.NewWriter(validOutput),
		statePath: filepath.Join(
			t.TempDir(),
			"missing",
			"state.json",
		),
		sourceFingerprint: endpointFingerprint(endpoint),
	}, validOptions, newSolrReader(endpoint, validOptions), io.Discard); err == nil || !strings.Contains(err.Error(), "create checkpoint temporary file") {
		t.Fatalf("runExportSession header error = %v", err)
	}
}

func TestExportCommitReportsSyncSeekAndCheckpointFailures(t *testing.T) {
	for _, test := range []struct {
		name      string
		file      func(*testing.T) *controlledExportFile
		statePath func(*testing.T) string
		want      string
	}{
		{name: "sync", file: func(t *testing.T) *controlledExportFile {
			f := newControlledExportFile(t)
			f.syncErr = errors.New("sync")
			return f
		}, want: "sync exported Solr page"},
		{name: "seek", file: func(t *testing.T) *controlledExportFile {
			f := newControlledExportFile(t)
			f.seekErr = errors.New("seek")
			return f
		}, want: "measure exported Solr page offset"},
		{name: "checkpoint", file: newControlledExportFile, statePath: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing", "state.json") }, want: "create checkpoint temporary file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := test.file(t)
			statePath := filepath.Join(t.TempDir(), "state.json")
			if test.statePath != nil {
				statePath = test.statePath(t)
			}
			session := &exportSession{
				output:    file,
				writer:    bufio.NewWriter(file),
				statePath: statePath,
				state: exportCheckpoint{
					Version:           1,
					SourceID:          "fixture",
					SourceFingerprint: "fixture",
					Cursor:            "*",
				},
			}
			if _, err := session.commitPage(
				solrPage{NextCursorMark: "next"},
				io.Discard,
			); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("commit page error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestExportPagesReturnsSourceMappingAndCursorFailures(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "source unavailable", body: "unavailable", want: "HTTP 503"},
		{
			name: "invalid source row",
			body: `{"response":{"numFound":1,"docs":[{}]},"nextCursorMark":"next"}`,
			want: "map source row",
		},
		{
			name: "missing cursor",
			body: `{"response":{"numFound":0,"docs":[]}}`,
			want: "nextCursorMark",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if test.name == "source unavailable" {
						http.Error(w, test.body, http.StatusServiceUnavailable)
						return
					}
					http.Error(w, test.body, http.StatusOK)
				}),
			)
			defer server.Close()
			endpoint, _ := url.Parse(server.URL)
			file := newControlledExportFile(t)
			session := &exportSession{
				output: file, writer: bufio.NewWriter(file),
				statePath: filepath.Join(t.TempDir(), "state.json"),
				state:     exportCheckpoint{Cursor: "*"},
			}
			err := session.exportPages(
				t.Context(),
				exportOptions{SourceID: "fixture", Rows: 1},
				newSolrReader(endpoint, exportOptions{}),
				io.Discard,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("exportPages error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSolrReaderUsesAuthenticatedCursorRequest(t *testing.T) {
	endpoint, _ := url.Parse("https://solr.example.test/select")
	reader := newSolrReader(endpoint, exportOptions{User: "reader", Password: "secret"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "reader" || password != "secret" {
			t.Errorf("Solr auth = %q:%q present=%t", user, password, ok)
		}
		if r.Method != http.MethodGet || r.URL.Query().Get("cursorMark") != "mark" ||
			r.URL.Query().Get("rows") != "3" {
			t.Errorf("Solr cursor request = %s %v", r.Method, r.URL.Query())
		}
		_, _ = io.WriteString(w, `{"response":{"numFound":0,"docs":[]},"nextCursorMark":"mark"}`)
	}))
	defer server.Close()
	endpoint, _ = url.Parse(server.URL)
	reader.endpoint = endpoint
	if _, err := reader.read(t.Context(), "mark", 3); err != nil {
		t.Fatalf("valid Solr response: %v", err)
	}
	server.Close()
}

func TestSolrReaderRejectsMalformedAndUnsuccessfulResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
		want string
	}{
		{name: "malformed JSON", body: "not-json", code: http.StatusOK, want: "decode Solr page"},
		{
			name: "unavailable",
			body: "unavailable",
			code: http.StatusServiceUnavailable,
			want: "HTTP 503",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(test.code)
					http.Error(w, test.body, http.StatusOK)
				}),
			)
			defer server.Close()
			endpoint, _ := url.Parse(server.URL)
			reader := newSolrReader(endpoint, exportOptions{})
			if _, err := reader.read(t.Context(), "*", 1); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("Solr response error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSolrReaderReportsTransportAndCancellation(t *testing.T) {
	reader := newSolrReader(&url.URL{Scheme: "http", Host: "invalid host"}, exportOptions{})
	closedServer := httptest.NewServer(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	)
	closedEndpoint, _ := url.Parse(closedServer.URL)
	closedServer.Close()
	reader.endpoint = closedEndpoint
	if _, err := reader.read(
		t.Context(),
		"*",
		1,
	); err == nil ||
		!strings.Contains(err.Error(), "request Solr page") {
		t.Fatalf("Solr transport error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reader.read(canceled, "*", 1); err == nil {
		t.Fatal("canceled Solr request succeeded")
	}
	reader.endpoint = &url.URL{Scheme: "http", Host: "invalid host"}
	if _, err := reader.read(
		t.Context(),
		"*",
		1,
	); err == nil ||
		!strings.Contains(err.Error(), "build solr request") {
		t.Fatalf("invalid request URL error = %v", err)
	}
}

func TestSolrReaderReportsResponseCloseFailure(t *testing.T) {
	reader := newSolrReader(&url.URL{Scheme: "https", Host: "solr.example.test"}, exportOptions{})
	reader.endpoint, _ = url.Parse("https://solr.example.test/select")
	closeFailure := errors.New("Solr body close failed")
	reader.client = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: errorClosingBody{
					Reader: strings.NewReader(`{"response":{"docs":[]},"nextCursorMark":"*"}`),
					err:    closeFailure,
				},
			}, nil
		}),
	}
	if _, err := reader.read(t.Context(), "*", 1); !errors.Is(err, closeFailure) {
		t.Fatalf("Solr response close error = %v", err)
	}
}

func TestExportProgressWritersReturnErrors(t *testing.T) {
	if err := writeJSONLine(errorWriter{}, map[string]string{"key": "value"}); err == nil {
		t.Fatal("JSON output writer failure was ignored")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"response":{"numFound":0,"docs":[]},"nextCursorMark":"*"}`)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	if err := runExportDryRun(
		t.Context(),
		newSolrReader(endpoint, exportOptions{}),
		errorWriter{},
	); err == nil {
		t.Fatal("dry-run progress writer failure was ignored")
	}
	failedServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "temporary source failure", http.StatusServiceUnavailable)
		}),
	)
	defer failedServer.Close()
	failedEndpoint, _ := url.Parse(failedServer.URL)
	if err := runExportDryRun(
		t.Context(),
		newSolrReader(failedEndpoint, exportOptions{}),
		io.Discard,
	); err == nil ||
		!strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("dry-run source failure = %v", err)
	}
}

func TestExportSessionWritersReturnErrors(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "output-*")
	if err != nil {
		t.Fatal(err)
	}
	closeMigrationFixture(t, file)
	session := &exportSession{
		output: file,
		writer: bufio.NewWriterSize(errorWriter{}, 1),
		state:  exportCheckpoint{Cursor: "*"},
	}
	if err := session.writeDocuments(solrPage{}, "fixture"); err != nil {
		t.Fatalf("empty document page: %v", err)
	}
	validRow := map[string]json.RawMessage{
		"sku": json.RawMessage(`"https://fixture.example.test/"`),
	}
	page := solrPage{}
	page.Response.Docs = []map[string]json.RawMessage{validRow}
	if err := session.writeDocuments(page, "fixture"); err == nil {
		t.Fatal("document writer failure was ignored")
	}
	if _, err := session.commitPage(solrPage{}, io.Discard); err == nil {
		t.Fatal("missing Solr cursor mark was accepted")
	}
	if _, err := session.commitPage(
		solrPage{NextCursorMark: "next"},
		io.Discard,
	); err == nil ||
		!strings.Contains(err.Error(), "flush exported Solr page") {
		t.Fatalf("flush failure = %v", err)
	}

	goodFile, err := os.CreateTemp(t.TempDir(), "output-good-*")
	if err != nil {
		t.Fatal(err)
	}
	closeMigrationFixture(t, goodFile)
	good := &exportSession{
		output:    goodFile,
		writer:    bufio.NewWriter(goodFile),
		statePath: filepath.Join(t.TempDir(), "state.json"),
		state: exportCheckpoint{
			Version:           1,
			SourceID:          "fixture",
			SourceFingerprint: "fingerprint",
			Cursor:            "*",
		},
	}
	if _, err := good.commitPage(
		solrPage{NextCursorMark: "next"},
		errorWriter{},
	); err == nil ||
		!strings.Contains(err.Error(), "write export progress") {
		t.Fatalf("commit progress failure = %v", err)
	}
}

func newControlledExportFile(t *testing.T) *controlledExportFile {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "controlled-export-*")
	if err != nil {
		t.Fatal(err)
	}
	closeMigrationFixture(t, file)
	return &controlledExportFile{File: file}
}

type controlledExportFile struct {
	*os.File
	chmodErr    error
	statErr     error
	truncateErr error
	seekErr     error
	syncErr     error
	writeErr    error
	closeErr    error
}

func (f *controlledExportFile) Chmod(mode os.FileMode) error {
	if f.chmodErr != nil {
		return f.chmodErr
	}
	if err := f.File.Chmod(mode); err != nil {
		return fmt.Errorf("chmod controlled export file: %w", err)
	}
	return nil
}

func (f *controlledExportFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	info, err := f.File.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat controlled export file: %w", err)
	}
	return info, nil
}

func (f *controlledExportFile) Truncate(size int64) error {
	if f.truncateErr != nil {
		return f.truncateErr
	}
	if err := f.File.Truncate(size); err != nil {
		return fmt.Errorf("truncate controlled export file: %w", err)
	}
	return nil
}

func (f *controlledExportFile) Seek(offset int64, whence int) (int64, error) {
	if f.seekErr != nil {
		return 0, f.seekErr
	}
	position, err := f.File.Seek(offset, whence)
	if err != nil {
		return 0, fmt.Errorf("seek controlled export file: %w", err)
	}
	return position, nil
}

func (f *controlledExportFile) Sync() error {
	if f.syncErr != nil {
		return f.syncErr
	}
	if err := f.File.Sync(); err != nil {
		return fmt.Errorf("sync controlled export file: %w", err)
	}
	return nil
}

func (f *controlledExportFile) Write(data []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	written, err := f.File.Write(data)
	if err != nil {
		return written, fmt.Errorf("write controlled export file: %w", err)
	}
	return written, nil
}

func (f *controlledExportFile) Close() error {
	fileErr := f.File.Close()
	return errors.Join(fileErr, f.closeErr)
}
