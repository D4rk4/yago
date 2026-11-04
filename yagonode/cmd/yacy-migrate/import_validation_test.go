package main

import (
	"bufio"
	"bytes"
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
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentimport"
)

func TestFollowImportCheckpointAndOptionBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := url.Parse("https://yago.example.test/admin/index/import")
	base := importCheckpoint{
		FollowExportCheckpoint: "export.state",
		DestinationFingerprint: destinationFingerprint(endpoint),
		Offset:                 3,
		FollowExportOffset:     3,
	}
	if err := validateFollowImportCheckpoint(
		base,
		true,
		info,
		endpoint,
		"export.state",
	); err != nil {
		t.Fatalf("valid follow checkpoint rejected: %v", err)
	}
	if err := validateFollowImportCheckpoint(
		importCheckpoint{},
		false,
		info,
		endpoint,
		"export.state",
	); err != nil {
		t.Fatalf("new follow checkpoint rejected: %v", err)
	}
	for _, state := range []importCheckpoint{
		{FollowExportCheckpoint: "other", Offset: 1},
		{FollowExportCheckpoint: "export.state", DestinationFingerprint: "other", Offset: 1},
		{FollowExportCheckpoint: "export.state", DestinationFingerprint: destinationFingerprint(endpoint), Offset: -1},
		{FollowExportCheckpoint: "export.state", DestinationFingerprint: destinationFingerprint(endpoint), Offset: info.Size() + 1},
		{FollowExportCheckpoint: "export.state", DestinationFingerprint: destinationFingerprint(endpoint), Offset: 2, FollowExportOffset: 1},
	} {
		if err := validateFollowImportCheckpoint(
			state,
			true,
			info,
			endpoint,
			"export.state",
		); err == nil {
			t.Errorf("invalid follow checkpoint %+v accepted", state)
		}
	}

	valid := importOptions{
		Input: path, Endpoint: endpoint.String(), APIKey: "key",
		FollowExportCheckpoint: "export.state", BatchRecords: 1,
	}
	for _, edit := range []func(*importOptions){
		func(o *importOptions) { o.DryRun = true },
		func(o *importOptions) { o.BatchRecords = -1 },
		func(o *importOptions) { o.BatchRecords = importMaximumRecords + 1 },
		func(o *importOptions) { o.MaxRecords = -1 },
		func(o *importOptions) { o.FollowExportCheckpoint = o.Input },
		func(o *importOptions) { o.State = "export.state" },
	} {
		opts := valid
		edit(&opts)
		if _, err := validateImportOptions(opts); err == nil {
			t.Errorf("invalid import options %+v accepted", opts)
		}
	}
}

func TestImportSessionFollowBoundariesWaitAndFileFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForFollowedExport(ctx); err == nil {
		t.Fatal("cancelled followed-export wait succeeded")
	}
	session := &importSession{options: importOptions{FollowExportCheckpoint: "missing"}}
	if err := session.refreshFollowedExport(); err == nil {
		t.Fatal("missing followed export checkpoint was accepted")
	}
	if _, err := session.finishOrWaitForExport(ctx, io.Discard); err == nil {
		t.Fatal("cancelled followed-export wait was ignored")
	}
	session.options.FollowExportCheckpoint = ""
	if done, err := session.finishOrWaitForExport(ctx, io.Discard); err != nil || !done {
		t.Fatalf("ordinary import completion = %t, %v", done, err)
	}
	for _, records := range []int{0, 1, importMaximumRecords} {
		session.options.BatchRecords = records
		got := session.batchLimit(0)
		want := records
		if want == 0 {
			want = importMaximumRecords
		}
		if got != want {
			t.Errorf("batch limit for configured=%d is %d, want %d", records, got, want)
		}
	}
	session.options.BatchRecords = 20
	if got := session.batchLimit(3); got != 3 {
		t.Fatalf("remaining-record batch limit = %d, want 3", got)
	}
	closedPath := filepath.Join(t.TempDir(), "closed")
	file, err := os.Create(closedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	session.file = file
	if err := session.refreshImportFileInfo(); err == nil {
		t.Fatal("closed input file was accepted")
	}
	if err := session.refreshFollowExportBoundary(); err == nil {
		t.Fatal("closed input file with a follow checkpoint was accepted")
	}
}

func TestOpenImportSessionRejectsMissingInputAndFinishedFollowFileErrors(t *testing.T) {
	missing := importOptions{
		Input:    filepath.Join(t.TempDir(), "missing.jsonl"),
		Endpoint: "https://yago.example.test/admin/index/import", APIKey: "key", BatchRecords: 1,
	}
	if _, err := openImportSession(missing); err == nil {
		t.Fatal("missing import input was accepted")
	}
	input := filepath.Join(t.TempDir(), "input.jsonl")
	writeInterchangeFixture(t, input, nil)
	file, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	session := &importSession{
		options: importOptions{FollowExportCheckpoint: "export.state"},
		file:    file, statePath: statePath, followExportComplete: true,
	}
	if done, err := session.finishOrWaitForExport(
		context.Background(),
		errorWriter{},
	); !done ||
		err == nil {
		t.Fatalf("finished follow progress write = %t, %v", done, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if done, err := session.finishOrWaitForExport(
		context.Background(),
		io.Discard,
	); !done ||
		err == nil {
		t.Fatalf("finished follow with closed input = %t, %v", done, err)
	}
}

func TestFollowImportCompletionBoundaryAndRecordRecrawlGuard(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.jsonl")
	writeInterchangeFixture(t, input, nil)
	file, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	info, _ := file.Stat()
	statePath := filepath.Join(t.TempDir(), "import.state")
	followPath := filepath.Join(t.TempDir(), "export.state")
	session := &importSession{
		options: importOptions{Input: input, State: statePath, FollowExportCheckpoint: followPath},
		file:    file, info: info, statePath: statePath, followExportComplete: true,
		state: importCheckpoint{Offset: 1}, followExportOffset: 2,
	}
	if done, err := session.finishOrWaitForExport(
		context.Background(),
		io.Discard,
	); !done ||
		err == nil {
		t.Fatalf("boundary mismatch completion = %t, %v", done, err)
	}
	session.state.Offset = session.followExportOffset
	session.statePath = filepath.Join(t.TempDir(), "missing", "state.json")
	if done, err := session.finishOrWaitForExport(
		context.Background(),
		io.Discard,
	); !done ||
		err == nil {
		t.Fatalf("checkpoint write failure = %t, %v", done, err)
	}
	session.statePath = filepath.Join(t.TempDir(), "state.json")
	if done, err := session.finishOrWaitForExport(
		context.Background(),
		errorWriter{},
	); !done ||
		err == nil {
		t.Fatalf("summary writer failure = %t, %v", done, err)
	}
	record := validImportRecord()
	future := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	record.NextVisitAt = &future
	record.Document.NormalizedURL = "https://bad%40host.example.test/page"
	record.Document.Metadata = map[string]string{
		documentimport.LegacyURLStatusMetadataKey: documentimport.LegacyURLStatusInvalidHostEscape,
	}
	if err := validateImportRecord(record); err == nil {
		t.Fatal("legacy malformed URL was allowed to recrawl")
	}
}

func TestOpenFollowImportRejectsCheckpointMismatchAndMissingExport(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.jsonl")
	writeInterchangeFixture(t, input, nil)
	statePath := filepath.Join(t.TempDir(), "import.state")
	endpoint := &url.URL{Scheme: "https", Host: "yago.example.test", Path: "/admin/index/import"}
	if err := saveCheckpoint(statePath, importCheckpoint{
		Version: 1, SourceID: "fixture-source",
		FollowExportCheckpoint: "different.export.state",
		DestinationFingerprint: destinationFingerprint(endpoint),
	}); err != nil {
		t.Fatal(err)
	}
	opts := importOptions{
		Input: input, Endpoint: endpoint.String(), APIKey: "key",
		State: statePath, FollowExportCheckpoint: filepath.Join(t.TempDir(), "export.state"),
	}
	if _, err := openImportSession(opts); err == nil {
		t.Fatal("checkpoint for another followed export was accepted")
	}
	opts.State = filepath.Join(t.TempDir(), "new-import.state")
	if _, err := openImportSession(opts); err == nil {
		t.Fatal("new import without the export checkpoint was accepted")
	}
	session := &importSession{options: opts}
	if err := session.importDocuments(context.Background(), io.Discard); err == nil {
		t.Fatal("import loop ignored the missing followed-export checkpoint")
	}
	if err := waitForFollowedExport(context.Background()); err != nil {
		t.Fatalf("active followed-export wait: %v", err)
	}
}

func TestFollowExportBoundaryChecksSourceOffsetsAndFileState(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.jsonl")
	writeInterchangeFixture(t, input, nil)
	file, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	info, _ := file.Stat()
	checkpointPath := filepath.Join(t.TempDir(), "export.state")
	session := &importSession{
		options: importOptions{FollowExportCheckpoint: checkpointPath},
		file:    file, info: info, state: importCheckpoint{SourceID: "source"},
	}
	if err := session.refreshFollowExportBoundary(); err == nil {
		t.Fatal("missing export checkpoint was accepted")
	}
	session.options.FollowExportCheckpoint = t.TempDir()
	if err := session.refreshFollowExportBoundary(); err == nil {
		t.Fatal("unreadable export checkpoint was accepted")
	}
	session.options.FollowExportCheckpoint = checkpointPath
	for _, state := range []exportCheckpoint{
		{Version: 1, SourceID: "other", SourceFingerprint: "fingerprint", Cursor: "*"},
		{Version: 1, SourceID: "source", SourceFingerprint: "changed", Cursor: "*"},
		{Version: 1, SourceID: "source", SourceFingerprint: "fingerprint", Cursor: "*", Offset: -1},
		{Version: 1, SourceID: "source", SourceFingerprint: "fingerprint", Cursor: "*", Offset: info.Size() + 1},
	} {
		if err := saveCheckpoint(checkpointPath, state); err != nil {
			t.Fatal(err)
		}
		session.state.FollowSourceFingerprint = "fingerprint"
		session.state.FollowExportOffset = 0
		session.state.Offset = 0
		if err := session.refreshFollowExportBoundary(); err == nil {
			t.Errorf("invalid export checkpoint %+v accepted", state)
		}
	}
	valid := exportCheckpoint{
		Version:           1,
		SourceID:          "source",
		SourceFingerprint: "fingerprint",
		Cursor:            "*",
		Offset:            info.Size(),
	}
	if err := saveCheckpoint(checkpointPath, valid); err != nil {
		t.Fatal(err)
	}
	session.state.FollowSourceFingerprint = ""
	if err := session.refreshFollowExportBoundary(); err != nil {
		t.Fatalf("valid export checkpoint rejected: %v", err)
	}
	if session.followExportOffset != info.Size() ||
		session.state.FollowSourceFingerprint != "fingerprint" {
		t.Fatalf("follow state was not advanced: %+v", session)
	}
	closed, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	session.file = closed
	if err := session.refreshFollowExportBoundary(); err == nil {
		t.Fatal("closed interchange file was accepted")
	}
}

func TestImportBatchRejectsRecordThatCannotBeEncoded(t *testing.T) {
	record := validImportRecord()
	outOfRange := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	record.NextVisitAt = &outOfRange
	session := new(importSession)
	_, _, err := session.nextBatchUsing(func() (*pendingRecord, bool, error) {
		return &pendingRecord{value: record, end: 10}, false, nil
	})
	if err == nil || !strings.Contains(err.Error(), "size import record") {
		t.Fatalf("unencodable record error = %v", err)
	}
}

func TestValidateImportOptionsAndCheckpoint(t *testing.T) {
	infoPath := filepath.Join(t.TempDir(), "input.jsonl")
	if err := os.WriteFile(infoPath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(infoPath)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := url.Parse("https://yago.example.test/admin/index/import")
	valid := importOptions{Input: infoPath, Endpoint: endpoint.String(), APIKey: "token"}

	for _, test := range []struct {
		name string
		edit func(*importOptions)
		want string
	}{
		{name: "missing input", edit: func(o *importOptions) { o.Input = "" }, want: "input path is required"},
		{name: "relative endpoint", edit: func(o *importOptions) { o.Endpoint = "/import" }, want: "absolute HTTP(S) URL"},
		{name: "credential endpoint", edit: func(o *importOptions) { o.Endpoint = "https://user:pass@yago.example.test/import" }, want: "embedded credentials"},
		{name: "fragment endpoint", edit: func(o *importOptions) { o.Endpoint += "#fragment" }, want: "embedded credentials"},
		{name: "missing API key", edit: func(o *importOptions) { o.APIKey = " " }, want: "YAGO_API_KEY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.edit(&options)
			if _, err := validateImportOptions(
				options,
			); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate options error = %v, want %q", err, test.want)
			}
		})
	}
	dryRun := valid
	dryRun.APIKey, dryRun.DryRun = "", true
	if _, err := validateImportOptions(dryRun); err != nil {
		t.Fatalf("dry-run should not need an API key: %v", err)
	}

	state := importCheckpoint{
		InputSize: info.Size(), InputModTime: info.ModTime().UnixNano(),
		DestinationFingerprint: destinationFingerprint(endpoint), Offset: info.Size(),
	}
	if err := validateImportCheckpoint(state, true, info, endpoint); err != nil {
		t.Fatalf("valid checkpoint: %v", err)
	}
	if err := validateImportCheckpoint(
		state,
		true,
		alteredFileInfo{FileInfo: info, size: info.Size() + 1},
		endpoint,
	); err == nil {
		t.Fatal("checkpoint for changed input size was accepted")
	}
	if err := validateImportCheckpoint(
		state,
		true,
		alteredFileInfo{FileInfo: info, modTime: info.ModTime().Add(time.Second)},
		endpoint,
	); err == nil {
		t.Fatal("checkpoint for changed input timestamp was accepted")
	}
	otherEndpoint, _ := url.Parse("https://other.example.test/admin/index/import")
	if err := validateImportCheckpoint(state, true, info, otherEndpoint); err == nil {
		t.Fatal("checkpoint for a different destination was accepted")
	}
	state.Offset = info.Size() + 1
	if err := validateImportCheckpoint(state, false, info, endpoint); err == nil {
		t.Fatal("checkpoint outside the input was accepted")
	}
	state.Offset = -1
	if err := validateImportCheckpoint(state, false, info, endpoint); err == nil {
		t.Fatal("negative checkpoint offset was accepted")
	}
}

func TestValidateInterchangeURLAcceptsYaCyFTPAndSMB(t *testing.T) {
	for _, rawURL := range []string{
		"ftp://archive.example.test/pub/item.pdf",
		"smb://archive.example.test/share/item.pdf",
	} {
		if err := validateInterchangeURL(rawURL); err != nil {
			t.Errorf("supported YaCy identity %q rejected: %v", rawURL, err)
		}
	}
}

func TestImportHeaderAndCheckpointLoadingRejectInvalidState(t *testing.T) {
	for _, line := range [][]byte{
		[]byte("not-json"),
		[]byte(`{"type":"document","version":1,"sourceId":"fixture"}`),
		[]byte(`{"type":"header","version":2,"sourceId":"fixture"}`),
		[]byte(`{"type":"header","version":1,"sourceId":" "}`),
	} {
		if _, err := parseInterchangeHeader(line); err == nil {
			t.Fatalf("invalid header %q was accepted", line)
		}
	}
	if header, err := parseInterchangeHeader(
		[]byte(`{"type":"header","version":1,"sourceId":"fixture"}`),
	); err != nil ||
		header.SourceID != "fixture" {
		t.Fatalf("valid header = %+v, %v", header, err)
	}

	missing, found, err := loadImportCheckpoint(filepath.Join(t.TempDir(), "missing"))
	if err != nil || found || missing.Version != 0 {
		t.Fatalf("missing checkpoint = %+v, %t, %v", missing, found, err)
	}
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadImportCheckpoint(path); err == nil {
		t.Fatal("malformed checkpoint was accepted")
	}
	if err := os.WriteFile(
		path,
		[]byte(`{"version":2,"sourceId":"fixture","offset":0}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadImportCheckpoint(path); err == nil {
		t.Fatal("unsupported checkpoint version was accepted")
	}
	if _, _, err := loadImportCheckpoint(t.TempDir()); err == nil {
		t.Fatal("checkpoint directory read failure was ignored")
	}
}

func TestImportSessionRejectsChangedOrMismatchedCheckpointHeader(t *testing.T) {
	input := filepath.Join(t.TempDir(), "source.jsonl")
	writeInterchangeFixture(t, input, []documentimport.Record{
		cliTestRecord("fixture", "https://fixture.example.test/"),
	})
	endpoint := "https://yago.example.test/admin/index/import"
	statePath := importCheckpointPath(input)
	info, err := os.Stat(input)
	if err != nil {
		t.Fatal(err)
	}
	parsedEndpoint, _ := url.Parse(endpoint)
	state := importCheckpoint{
		Version: 1, SourceID: "different-source",
		DestinationFingerprint: destinationFingerprint(parsedEndpoint),
		InputSize:              info.Size(), InputModTime: info.ModTime().UnixNano(),
		Offset: info.Size(),
	}
	if err := saveCheckpoint(statePath, state); err != nil {
		t.Fatal(err)
	}
	if _, err := openImportSession(
		importOptions{Input: input, Endpoint: endpoint, APIKey: "token"},
	); err == nil ||
		!strings.Contains(err.Error(), "header does not match") {
		t.Fatalf("mismatched checkpoint header error = %v", err)
	}

	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openImportSession(
		importOptions{Input: input, Endpoint: endpoint, APIKey: "token"},
	); err == nil ||
		!strings.Contains(err.Error(), "invalid versioned") {
		t.Fatalf("invalid initial header error = %v", err)
	}
}

func TestOpenImportSessionReportsFileAndCheckpointFailures(t *testing.T) {
	base := importOptions{Endpoint: "https://yago.example.test/import", APIKey: "token"}
	if _, err := openImportSession(
		importOptions{},
	); err == nil ||
		!strings.Contains(err.Error(), "input path is required") {
		t.Fatalf("invalid session options error = %v", err)
	}
	base.Input = filepath.Join(t.TempDir(), "missing.jsonl")
	if _, err := openImportSession(
		base,
	); err == nil ||
		!strings.Contains(err.Error(), "open interchange file") {
		t.Fatalf("missing input error = %v", err)
	}

	input := filepath.Join(t.TempDir(), "source.jsonl")
	writeInterchangeFixture(t, input, nil)
	base.Input = input
	base.State = t.TempDir()
	if _, err := openImportSession(
		base,
	); err == nil ||
		!strings.Contains(err.Error(), "read import checkpoint") {
		t.Fatalf("checkpoint read error = %v", err)
	}
	base.State = filepath.Join(t.TempDir(), "missing-parent", "checkpoint.json")
	if _, err := openImportSession(
		base,
	); err == nil ||
		!strings.Contains(err.Error(), "create checkpoint temporary file") {
		t.Fatalf("checkpoint creation error = %v", err)
	}
	if _, err := importInputInfo(
		importStatFailure{err: errors.New("stat failed")},
	); err == nil ||
		!strings.Contains(err.Error(), "inspect interchange input") {
		t.Fatalf("input stat failure = %v", err)
	}
	closed, err := os.CreateTemp(t.TempDir(), "closed-before-stat-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openImportSessionUsing(importOptions{
		Input: "fixture", Endpoint: "https://yago.example.test/import", APIKey: "token",
	}, func(string) (*os.File, error) { return closed, nil }); err == nil || !strings.Contains(err.Error(), "inspect interchange input") {
		t.Fatalf("open session stat failure = %v", err)
	}
}

func TestImportInputCloseErrorsArePreserved(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "closed-input-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	primary := errors.New("prior import failure")
	closeErr := closeImportInput(file, "close interchange input", primary)
	if !errors.Is(closeErr, primary) ||
		!strings.Contains(closeErr.Error(), "close interchange input") {
		t.Fatalf("close error = %v", closeErr)
	}
}

func TestImportHeaderReaderRejectsOversizedAndChangedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.jsonl")
	if err := os.WriteFile(
		path,
		[]byte(strings.Repeat("x", documentimport.MaximumRequestBytes+1)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	session := &importSession{
		file: file, info: info, reader: bufio.NewReader(file),
		endpoint:  &url.URL{Scheme: "https", Host: "yago.example.test"},
		statePath: filepath.Join(t.TempDir(), "checkpoint.json"),
	}
	if err := session.readInitialHeader(); err == nil ||
		!strings.Contains(err.Error(), "line exceeds batch size") {
		t.Fatalf("oversized header error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		path,
		[]byte(`{"type":"header","version":1,"sourceId":"fixture"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	file, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err = file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	session = &importSession{
		file:      file,
		info:      alteredFileInfo{FileInfo: info, size: info.Size() + 1},
		reader:    bufio.NewReader(file),
		endpoint:  &url.URL{Scheme: "https", Host: "yago.example.test"},
		statePath: filepath.Join(t.TempDir(), "checkpoint.json"),
	}
	if err := session.readInitialHeader(); err == nil ||
		!strings.Contains(err.Error(), "changed during import") {
		t.Fatalf("changed input during header error = %v", err)
	}
	_ = file.Close()
}

func TestImportSessionBatchesAtRecordLimitAndPreservesPendingRows(t *testing.T) {
	input := filepath.Join(t.TempDir(), "many.jsonl")
	records := make([]documentimport.Record, importMaximumRecords+1)
	for index := range records {
		records[index] = cliTestRecord(
			fmt.Sprintf("fixture-%d", index),
			fmt.Sprintf("https://fixture-%d.example.test/", index),
		)
	}
	writeInterchangeFixture(t, input, records)
	session, err := openImportSession(importOptions{
		Input: input, Endpoint: "https://yago.example.test/admin/index/import", APIKey: "token",
	})
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	closeMigrationFixture(t, session.file)
	first, firstEnd, err := session.nextBatch()
	if err != nil || len(first) != importMaximumRecords || firstEnd == 0 || session.pending != nil {
		t.Fatalf(
			"first batch size=%d end=%d pending=%v err=%v",
			len(first),
			firstEnd,
			session.pending != nil,
			err,
		)
	}
	second, secondEnd, err := session.nextBatch()
	if err != nil || len(second) != 1 || second[0].ID != records[importMaximumRecords].ID ||
		secondEnd <= firstEnd {
		t.Fatalf("second batch=%#v end=%d err=%v", second, secondEnd, err)
	}
	empty, _, err := session.nextBatch()
	if err != nil || len(empty) != 0 {
		t.Fatalf("EOF batch = %#v, %v", empty, err)
	}

	tooLarge := validImportRecord()
	tooLarge.Document.ExtractedText = strings.Repeat("x", 3<<20)
	oversized := &importSession{pending: &pendingRecord{value: tooLarge}}
	if _, _, err := oversized.nextBatchUsingLimit(oversized.nextRecord, 3<<20); err == nil ||
		!strings.Contains(err.Error(), "exceeds the Yago import batch limit") {
		t.Fatalf("oversized single-record batch error = %v", err)
	}
}

func TestImportSessionHonorsConfiguredBatchRecordLimit(t *testing.T) {
	input := filepath.Join(t.TempDir(), "small-batches.jsonl")
	records := []documentimport.Record{
		cliTestRecord("one", "https://one.example.test/"),
		cliTestRecord("two", "https://two.example.test/"),
		cliTestRecord("three", "https://three.example.test/"),
	}
	writeInterchangeFixture(t, input, records)
	session, err := openImportSession(importOptions{
		Input: input, Endpoint: "https://yago.example.test/admin/index/import",
		APIKey: "token", BatchRecords: 1,
	})
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	closeMigrationFixture(t, session.file)
	for index, wantID := range []string{"one", "two", "three"} {
		batch, _, err := session.nextBatch()
		if err != nil || len(batch) != 1 || batch[0].ID != wantID {
			t.Fatalf("batch %d = %#v, want %q; error=%v", index, batch, wantID, err)
		}
	}
	if batch, _, err := session.nextBatch(); err != nil || len(batch) != 0 {
		t.Fatalf("final batch = %#v, %v", batch, err)
	}
}

func TestImportSessionUsesExactEncodedRequestByteLimit(t *testing.T) {
	records := []documentimport.Record{
		cliTestRecord("bounded-a", "https://bounded.example.test/a"),
		cliTestRecord("bounded-b", "https://bounded.example.test/b"),
	}
	records[0].Document.ExtractedText = "quotes: \"; slash: \\; newline:\n; HTML: <&; separator: \u2028"
	records[1].Document.ExtractedText = "second record"
	exactRequest, err := json.Marshal(documentimport.Request{Version: 1, Records: records})
	if err != nil {
		t.Fatalf("marshal expected request: %v", err)
	}
	requestOne, err := json.Marshal(documentimport.Request{Version: 1, Records: records[:1]})
	if err != nil {
		t.Fatalf("marshal one-record request: %v", err)
	}
	if len(exactRequest) <= len(requestOne) {
		t.Fatalf(
			"two-record request size %d is not larger than one-record size %d",
			len(exactRequest),
			len(requestOne),
		)
	}

	for _, test := range []struct {
		name        string
		maximumSize int
		wantRecords int
		wantPending bool
	}{
		{
			name:        "exact request limit is accepted",
			maximumSize: len(exactRequest),
			wantRecords: 2,
		},
		{
			name:        "one byte below limit defers the second record",
			maximumSize: len(exactRequest) - 1,
			wantRecords: 1,
			wantPending: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "bounded.jsonl")
			writeInterchangeFixture(t, input, records)
			session, err := openImportSession(importOptions{
				Input: input, Endpoint: "https://yago.example.test/import", APIKey: "token",
			})
			if err != nil {
				t.Fatalf("open session: %v", err)
			}
			closeMigrationFixture(t, session.file)
			batch, _, err := session.nextBatchUsingLimit(session.nextRecord, test.maximumSize)
			if err != nil || len(batch) != test.wantRecords ||
				(session.pending != nil) != test.wantPending {
				t.Fatalf("batch size=%d pending=%t, want size=%d pending=%t; error=%v",
					len(batch), session.pending != nil, test.wantRecords, test.wantPending, err)
			}
			actualRequest, err := json.Marshal(documentimport.Request{Version: 1, Records: batch})
			if err != nil || len(actualRequest) > test.maximumSize {
				t.Fatalf("encoded request size=%d, limit=%d, error=%v",
					len(actualRequest), test.maximumSize, err)
			}
		})
	}
}

func TestImportSessionRejectsMissingRecordFromReader(t *testing.T) {
	session := new(importSession)
	_, _, err := session.nextBatchUsing(func() (*pendingRecord, bool, error) {
		return nil, false, nil
	})
	if err == nil || !strings.Contains(err.Error(), "interchange reader returned no record") {
		t.Fatalf("missing reader record error = %v", err)
	}
}

func TestImportSessionDefersRecordWhenByteLimitIsReached(t *testing.T) {
	input := filepath.Join(t.TempDir(), "large-records.jsonl")
	records := make([]documentimport.Record, 5)
	for index := range records {
		records[index] = cliTestRecord(
			fmt.Sprintf("large-%d", index),
			fmt.Sprintf("https://large-%d.example.test/", index),
		)
		records[index].Document.ExtractedText = strings.Repeat(
			"x",
			1<<20,
		)
	}
	writeInterchangeFixture(t, input, records)
	session, err := openImportSession(importOptions{
		Input: input, Endpoint: "https://yago.example.test/import", APIKey: "token",
	})
	if err != nil {
		t.Fatalf("open large input: %v", err)
	}
	closeMigrationFixture(t, session.file)
	first, firstEnd, err := session.nextBatchUsingLimit(session.nextRecord, 3<<20)
	if err != nil || len(first) != 2 || session.pending == nil ||
		firstEnd != session.pending.end-int64(len(mustEncodeRecordLine(t, records[2]))) {
		t.Fatalf(
			"bounded first batch size=%d end=%d pending=%v err=%v",
			len(first),
			firstEnd,
			session.pending != nil,
			err,
		)
	}
	second, secondEnd, err := session.nextBatchUsingLimit(session.nextRecord, 3<<20)
	if err != nil || len(second) != 2 || second[0].ID != records[2].ID ||
		second[1].ID != records[3].ID ||
		secondEnd <= firstEnd {
		t.Fatalf("pending batch=%d end=%d err=%v", len(second), secondEnd, err)
	}
	third, thirdEnd, err := session.nextBatchUsingLimit(session.nextRecord, 3<<20)
	if err != nil || len(third) != 1 || third[0].ID != records[4].ID || thirdEnd <= secondEnd {
		t.Fatalf("final deferred batch=%d end=%d err=%v", len(third), thirdEnd, err)
	}
}

func TestValidateImportFileRejectsBadRecordsAndOversizedLines(t *testing.T) {
	valid := validImportRecord()
	encoded, err := json.Marshal(interchangeRecord{Type: "document", Version: 1, Record: valid})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateImportFile(
		bufio.NewReader(bytes.NewReader(encoded)),
		10,
		io.Discard,
	); err != nil {
		t.Fatalf("valid final line without newline: %v", err)
	}
	for _, line := range []string{
		"not-json\n",
		`{"type":"header","version":1}` + "\n",
		`{"type":"document","version":1,"record":{"id":"bad","document":{"normalizedURL":"file:///tmp/x"}}}` + "\n",
		strings.Repeat("x", documentimport.MaximumRequestBytes+1),
	} {
		if err := validateImportFile(
			bufio.NewReader(strings.NewReader(line)),
			7,
			io.Discard,
		); err == nil {
			t.Errorf("invalid dry-run record %q was accepted", line[:min(len(line), 40)])
		}
	}
}

func TestImportNextRecordRejectsMalformedEnvelopesAndRecords(t *testing.T) {
	for _, line := range []string{
		"not-json\n",
		`{"type":"header","version":1,"sourceId":"fixture"}` + "\n",
		`{"type":"document","version":2,"record":{}}` + "\n",
		`{"type":"document","version":1,"record":{"id":"bad","document":{"normalizedURL":"file:///tmp/x"}}}` + "\n",
	} {
		session := &importSession{reader: bufio.NewReader(strings.NewReader(line))}
		if _, _, err := session.nextRecord(); err == nil {
			t.Errorf("invalid record line %q was accepted", line)
		}
	}
	session := &importSession{reader: bufio.NewReader(strings.NewReader(""))}
	if record, eof, err := session.nextRecord(); record != nil || !eof || err != nil {
		t.Fatalf("empty input = record %v, eof %t, err %v", record, eof, err)
	}
	pending := &pendingRecord{value: validImportRecord(), end: 9}
	session.pending = pending
	if record, eof, err := session.nextRecord(); err != nil || eof || record != pending ||
		session.pending != nil {
		t.Fatalf("pending record = %v, eof %t, err %v", record, eof, err)
	}
	session.reader = bufio.NewReader(importErrorReader{})
	if _, _, err := session.nextRecord(); err == nil ||
		!strings.Contains(err.Error(), "read interchange record") {
		t.Fatalf("reader failure = %v", err)
	}
}

func TestRestoreImportCheckpointRejectsUnreadableAndMismatchedHeaders(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		want string
	}{
		{name: "empty header", data: "", want: "read interchange header"},
		{name: "bad JSON header", data: "not-json\n", want: "header does not match"},
		{name: "different source", data: `{"type":"header","version":1,"sourceId":"other"}` + "\n", want: "header does not match"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.jsonl")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			closeMigrationFixture(t, file)
			session := &importSession{
				file: file, reader: bufio.NewReader(file),
				state: importCheckpoint{SourceID: "fixture", Offset: int64(len(test.data))},
			}
			if err := session.restoreCheckpointHeader(); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("restore error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRestoreImportCheckpointRejectsInvalidOffsets(t *testing.T) {
	closedPath := filepath.Join(t.TempDir(), "closed.jsonl")
	if err := os.WriteFile(
		closedPath,
		[]byte(`{"type":"header","version":1,"sourceId":"fixture"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	closed, err := os.Open(closedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (&importSession{file: closed}).restoreCheckpointHeader(); err == nil ||
		!strings.Contains(err.Error(), "seek interchange header") {
		t.Fatalf("closed input seek error = %v", err)
	}
	negativePath := filepath.Join(t.TempDir(), "negative.jsonl")
	if err := os.WriteFile(
		negativePath,
		[]byte(`{"type":"header","version":1,"sourceId":"fixture"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	negative, err := os.Open(negativePath)
	if err != nil {
		t.Fatal(err)
	}
	closeMigrationFixture(t, negative)
	if err := (&importSession{
		file: negative, reader: bufio.NewReader(negative),
		state: importCheckpoint{SourceID: "fixture", Offset: -1},
	}).restoreCheckpointHeader(); err == nil || !strings.Contains(err.Error(), "seek import checkpoint offset") {
		t.Fatalf("invalid checkpoint seek error = %v", err)
	}
}

func TestValidateImportRecordRejectsEachUnsupportedField(t *testing.T) {
	tests := []struct {
		name string
		edit func(*documentimport.Record)
	}{
		{name: "blank ID", edit: func(r *documentimport.Record) { r.ID = " " }},
		{
			name: "oversized ID",
			edit: func(r *documentimport.Record) { r.ID = strings.Repeat("x", 513) },
		},
		{name: "oversized text", edit: func(r *documentimport.Record) {
			r.Document.ExtractedText = strings.Repeat(
				"x",
				documentimport.MaximumRecordTextBytes+1,
			)
		}},
		{name: "oversized title", edit: func(r *documentimport.Record) {
			r.Document.Title = strings.Repeat("x", yagocrawlcontract.MaximumDocumentTitleBytes+1)
		}},
		{name: "too many headings", edit: func(r *documentimport.Record) {
			r.Document.Headings = make([]string, documentimport.MaximumRecordHeadings+1)
		}},
		{name: "oversized heading", edit: func(r *documentimport.Record) {
			r.Document.Headings = []string{
				strings.Repeat("x", documentimport.MaximumRecordHeadingBytes+1),
			}
		}},
		{
			name: "missing normalized URL",
			edit: func(r *documentimport.Record) { r.Document.NormalizedURL = "" },
		},
		{
			name: "invalid normalized URL",
			edit: func(r *documentimport.Record) { r.Document.NormalizedURL = "file:///tmp/private" },
		},
		{
			name: "invalid canonical URL",
			edit: func(r *documentimport.Record) { r.Document.CanonicalURL = "https://user:secret@example.test/" },
		},
		{name: "too many outlinks", edit: func(r *documentimport.Record) {
			r.Document.Outlinks = make([]string, documentimport.MaximumRecordOutlinks+1)
		}},
		{name: "too many anchors", edit: func(r *documentimport.Record) {
			r.Document.Inlinks = make(
				[]yagocrawlcontract.AnchorText,
				documentimport.MaximumRecordInlinks+1,
			)
		}},
		{name: "too much metadata count", edit: func(r *documentimport.Record) {
			r.Document.Metadata = make(
				map[string]string,
				yagocrawlcontract.MaximumDocumentMetadata+1,
			)
			for i := 0; i <= yagocrawlcontract.MaximumDocumentMetadata; i++ {
				r.Document.Metadata[fmt.Sprintf("key-%d", i)] = "v"
			}
		}},
		{name: "too much metadata bytes", edit: func(r *documentimport.Record) {
			r.Document.Metadata = map[string]string{
				"large": strings.Repeat("x", documentimport.MaximumRecordMetadataBytes),
			}
		}},
		{
			name: "zero recrawl time",
			edit: func(r *documentimport.Record) { r.NextVisitAt = new(time.Time) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := validImportRecord()
			test.edit(&record)
			if err := validateImportRecord(record); err == nil {
				t.Fatal("unsupported import record was accepted")
			}
		})
	}
	if err := validateImportRecord(validImportRecord()); err != nil {
		t.Fatalf("valid import record rejected: %v", err)
	}
}

func TestValidateImportRecordAcceptsMaximumMigrationText(t *testing.T) {
	record := validImportRecord()
	record.Document.ExtractedText = strings.Repeat("x", documentimport.MaximumRecordTextBytes)
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("maximum migration text rejected: %v", err)
	}
}

func TestValidateImportRecordAcceptsYaCyOutlinksBeyondCrawlerLimit(t *testing.T) {
	record := validImportRecord()
	record.Document.Outlinks = make([]string, 12_447)
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("YaCy source outlinks rejected at migration boundary: %v", err)
	}
}

func TestValidateImportRecordKeepsLegacyEncodedAtHostOutOfRecrawl(t *testing.T) {
	const rawURL = "https://bad%40host.example.test/path"
	status := documentimport.PreservableLegacyURLStatus(rawURL)
	record := cliTestRecord("legacy-opaque-url", rawURL)
	record.Document.Metadata = map[string]string{
		documentimport.LegacyURLStatusMetadataKey: status,
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate legacy opaque record: %v", err)
	}
	if record.NextVisitAt != nil || !documentimport.IsPreservableLegacyURL(rawURL) {
		t.Fatal("legacy opaque URL must remain ineligible for recrawl")
	}
}

func TestValidateInterchangeURLRejectsUnsafeURLs(t *testing.T) {
	for _, raw := range []string{
		"https://example.test/" + strings.Repeat("x", yagocrawlcontract.MaximumCrawlURLBytes),
		"https://example.test/%zz",
		"file:///tmp/private",
		"https:/missing-host",
		"https://user:secret@example.test/",
		"https://example.test/#fragment",
	} {
		if err := validateInterchangeURL(raw); err == nil {
			t.Errorf("unsafe URL %q was accepted", raw)
		}
	}
	if err := validateInterchangeURL("https://example.test/path?q=1"); err != nil {
		t.Fatalf("valid URL rejected: %v", err)
	}
}

func TestReadBoundedLineHandlesEOFAndOversizedLines(t *testing.T) {
	line, err := readBoundedLine(bufio.NewReader(strings.NewReader("short\n")))
	if err != nil || string(line) != "short\n" {
		t.Fatalf("newline line = %q, %v", line, err)
	}
	line, err = readBoundedLine(bufio.NewReader(strings.NewReader("final")))
	if !errors.Is(err, io.EOF) || string(line) != "final" {
		t.Fatalf("final partial line = %q, %v", line, err)
	}
	line, err = readBoundedLine(
		bufio.NewReader(
			strings.NewReader(strings.Repeat("x", documentimport.MaximumRequestBytes+1)),
		),
	)
	if err == nil || !strings.Contains(err.Error(), "line exceeds batch size") || line != nil {
		t.Fatalf("oversized line = %d bytes, %v", len(line), err)
	}
}

func TestValidateImportResultsAndRecordCounters(t *testing.T) {
	requested := []documentimport.Record{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	for _, results := range [][]documentimport.Result{
		{{ID: "a", Status: "imported"}},
		{{ID: "wrong", Status: "imported"}, {ID: "b", Status: "already-present"}, {ID: "c", Status: "conflict"}},
		{{ID: "a", Status: "unknown"}, {ID: "b", Status: "already-present"}, {ID: "c", Status: "conflict"}},
	} {
		if err := validateImportResults(requested, results); err == nil {
			t.Errorf("invalid receipt %#v was accepted", results)
		}
	}
	valid := []documentimport.Result{
		{ID: "a", Status: "imported"},
		{ID: "b", Status: "already-present"},
		{ID: "c", Status: "conflict"},
	}
	if err := validateImportResults(requested, valid); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}
	session := &importSession{}
	session.recordResults(valid)
	if session.state.Imported != 1 || session.state.Present != 1 || session.state.Conflicts != 1 {
		t.Fatalf("recorded import counts = %+v", session.state)
	}
}

func TestImportProgressWritersReportOutputFailures(t *testing.T) {
	state := importCheckpoint{Records: 2, Imported: 1, Present: 1}
	if err := writeImportProgress(errorWriter{}, state); err == nil {
		t.Fatal("progress write failure was ignored")
	}
	if err := writeImportSummary(errorWriter{}, state); err == nil {
		t.Fatal("summary write failure was ignored")
	}
	if err := validateImportFile(
		bufio.NewReader(strings.NewReader("")),
		0,
		errorWriter{},
	); err == nil {
		t.Fatal("dry-run progress write failure was ignored")
	}
}

func TestSendImportBatchReportsTransportAndResponseFailures(t *testing.T) {
	request := []documentimport.Record{validImportRecord()}
	clientFor := func(fn func(*http.Request) (*http.Response, error)) *http.Client {
		return &http.Client{Transport: roundTripFunc(fn)}
	}
	if _, err := sendImportBatch(
		t.Context(),
		http.DefaultClient,
		"http://[::1",
		"token",
		request,
	); err == nil {
		t.Fatal("invalid endpoint was accepted")
	}
	transportFailure := errors.New("synthetic transport failure")
	if _, err := sendImportBatch(
		t.Context(),
		clientFor(func(*http.Request) (*http.Response, error) {
			return nil, transportFailure
		}),
		"https://yago.example.test/import",
		"token",
		request,
	); !errors.Is(
		err,
		transportFailure,
	) {
		t.Fatalf("transport error = %v", err)
	}
	for _, response := range []*http.Response{
		{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("busy"))},
		{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("not-json"))},
	} {
		client := clientFor(func(*http.Request) (*http.Response, error) { return response, nil })
		if _, err := sendImportBatch(
			t.Context(),
			client,
			"https://yago.example.test/import",
			"token",
			request,
		); err == nil {
			t.Fatalf("invalid response with status %d was accepted", response.StatusCode)
		}
	}
	closeFailure := errors.New("response close failed")
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: errorClosingBody{
			Reader: strings.NewReader(`{"results":[]}`),
			err:    closeFailure,
		},
	}
	if _, err := sendImportBatch(
		t.Context(),
		clientFor(func(*http.Request) (*http.Response, error) {
			return response, nil
		}),
		"https://yago.example.test/import",
		"token",
		request,
	); !errors.Is(
		err,
		closeFailure,
	) {
		t.Fatalf("response close error = %v", err)
	}
}

func TestImportCorpusDoesNotAdvanceCheckpointAfterInputMutation(t *testing.T) {
	input := filepath.Join(t.TempDir(), "mutable.jsonl")
	writeInterchangeFixture(
		t,
		input,
		[]documentimport.Record{cliTestRecord("row", "https://fixture.example.test/")},
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = os.WriteFile(
			input,
			[]byte("source changed while the import request was in flight"),
			0o600,
		)
		_ = json.NewEncoder(w).
			Encode(importResponse{Results: []documentimport.Result{{ID: "row", Status: "imported"}}})
	}))
	defer server.Close()
	statePath := filepath.Join(t.TempDir(), "progress.json")
	err := importCorpus(t.Context(), importOptions{
		Input: input, Endpoint: server.URL, APIKey: "token", State: statePath,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "changed during import") {
		t.Fatalf("mutated input import error = %v", err)
	}
	state, found, checkpointErr := loadImportCheckpoint(statePath)
	if checkpointErr != nil || !found || state.Records != 0 {
		t.Fatalf(
			"mutation advanced checkpoint: state=%+v found=%t err=%v",
			state,
			found,
			checkpointErr,
		)
	}
}

func TestImportCorpusCheckpointsAcceptedBatchBeforeProgressOutput(t *testing.T) {
	input := filepath.Join(t.TempDir(), "progress-error.jsonl")
	writeInterchangeFixture(
		t,
		input,
		[]documentimport.Record{cliTestRecord("row", "https://fixture.example.test/")},
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).
			Encode(importResponse{Results: []documentimport.Result{{ID: "row", Status: "imported"}}})
	}))
	defer server.Close()
	statePath := filepath.Join(t.TempDir(), "progress.json")
	err := importCorpus(t.Context(), importOptions{
		Input: input, Endpoint: server.URL, APIKey: "token", State: statePath,
	}, errorWriter{})
	if err == nil || !strings.Contains(err.Error(), "write import progress") {
		t.Fatalf("progress failure = %v", err)
	}
	state, found, checkpointErr := loadImportCheckpoint(statePath)
	if checkpointErr != nil || !found || state.Records != 1 {
		t.Fatalf(
			"accepted destination batch was not checkpointed before progress: state=%+v found=%t err=%v",
			state,
			found,
			checkpointErr,
		)
	}
}

func TestImportCorpusReportsCheckpointFailureAfterDestinationReceipt(t *testing.T) {
	input := filepath.Join(t.TempDir(), "checkpoint-failure.jsonl")
	writeInterchangeFixture(
		t,
		input,
		[]documentimport.Record{cliTestRecord("row", "https://fixture.example.test/")},
	)
	statePath := filepath.Join(t.TempDir(), "progress.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := os.Remove(statePath); err != nil {
			t.Errorf("remove checkpoint for failure injection: %v", err)
		}
		if err := os.Mkdir(statePath, 0o700); err != nil {
			t.Errorf("replace checkpoint with directory: %v", err)
		}
		_ = json.NewEncoder(w).
			Encode(importResponse{Results: []documentimport.Result{{ID: "row", Status: "imported"}}})
	}))
	defer server.Close()
	err := importCorpus(t.Context(), importOptions{
		Input: input, Endpoint: server.URL, APIKey: "token", State: statePath,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "replace checkpoint") {
		t.Fatalf("checkpoint failure after destination receipt = %v", err)
	}
	entries, globErr := filepath.Glob(
		filepath.Join(filepath.Dir(statePath), ".yacy-migrate-checkpoint-*"),
	)
	if globErr != nil || len(entries) != 0 {
		t.Fatalf("temporary checkpoints were not cleaned: %#v, %v", entries, globErr)
	}
}

func TestVerifyImportFileUnchangedDetectsMutationAndClosedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.jsonl")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(
		path,
		original.ModTime().Add(time.Second),
		original.ModTime().Add(time.Second),
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyImportFileUnchanged(file, original); err == nil {
		t.Fatal("input mutation was not detected")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyImportFileUnchanged(file, original); err == nil {
		t.Fatal("closed input file was accepted")
	}
}

func validImportRecord() documentimport.Record {
	return documentimport.Record{
		ID: "fixture-row",
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: "https://fixture.example.test/page",
			ContentHash:   "fixture-hash",
			ExtractedText: "sanitized fixture text",
		},
	}
}

func mustEncodeRecordLine(t *testing.T, record documentimport.Record) []byte {
	t.Helper()
	line, err := json.Marshal(interchangeRecord{Type: "document", Version: 1, Record: record})
	if err != nil {
		t.Fatal(err)
	}
	return append(line, '\n')
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("synthetic writer failure") }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type errorClosingBody struct {
	io.Reader
	err error
}

func (b errorClosingBody) Close() error { return b.err }

type alteredFileInfo struct {
	os.FileInfo
	size    int64
	modTime time.Time
}

type importErrorReader struct{}

func (importErrorReader) Read(
	[]byte,
) (int, error) {
	return 0, errors.New("synthetic read failure")
}

type importStatFailure struct{ err error }

func (f importStatFailure) Stat() (os.FileInfo, error) { return nil, f.err }

func (f alteredFileInfo) Size() int64 {
	if f.size != 0 {
		return f.size
	}
	return f.FileInfo.Size()
}

func (f alteredFileInfo) ModTime() time.Time {
	if !f.modTime.IsZero() {
		return f.modTime
	}
	return f.FileInfo.ModTime()
}
