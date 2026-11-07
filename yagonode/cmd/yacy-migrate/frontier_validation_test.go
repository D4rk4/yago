package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
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

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestFrontierOptionsEndpointAndSourceIDValidation(t *testing.T) {
	base := frontierImportOptions{
		Input:        "frontier",
		Profiles:     "profiles",
		BatchRecords: 1,
		DryRun:       true,
	}
	for _, edit := range []func(*frontierImportOptions){
		func(o *frontierImportOptions) { o.Input = "" },
		func(o *frontierImportOptions) { o.Profiles = "" },
		func(o *frontierImportOptions) { o.BatchRecords = 0 },
		func(o *frontierImportOptions) {
			o.BatchRecords = yagocrawlcontract.MaximumCrawlOrderImportBatchRecords + 1
		},
		func(o *frontierImportOptions) { o.MaxRecords = -1 },
		func(o *frontierImportOptions) { o.DryRun = false },
	} {
		opts := base
		edit(&opts)
		if err := validateFrontierImportOptions(opts); err == nil {
			t.Errorf("invalid options %+v were accepted", opts)
		}
	}
	live := base
	live.DryRun, live.APIKey = false, "key"
	if err := validateFrontierImportOptions(live); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
	if endpoint, err := validateFrontierEndpoint("", true); err != nil ||
		endpoint.Path != "/admin/crawl/frontier/import" {
		t.Fatalf("dry-run endpoint = %v, %v", endpoint, err)
	}
	for _, raw := range []string{
		"", "ftp://example.test/admin/crawl/frontier/import",
		"https:///admin/crawl/frontier/import",
		"https://user:secret@example.test/admin/crawl/frontier/import",
		"https://example.test/admin/crawl/frontier/import?x=1",
		"https://example.test/admin/crawl/frontier/import#fragment",
		"https://example.test/admin/crawl/frontier/other", "://bad",
	} {
		if _, err := validateFrontierEndpoint(raw, false); err == nil {
			t.Errorf("endpoint %q was accepted", raw)
		}
	}
	if _, err := validateFrontierEndpoint(
		"https://example.test/admin/crawl/frontier/import",
		false,
	); err != nil {
		t.Fatalf("valid endpoint rejected: %v", err)
	}
	for _, value := range []string{"A", "source-1", "source_name", "source.name"} {
		if !validFrontierSourceID(value) {
			t.Errorf("valid source ID %q rejected", value)
		}
	}
	for _, value := range []string{"", " leading", "trailing ", "bad/id", strings.Repeat("x", yagocrawlcontract.MaximumCrawlOrderImportSourceID+1)} {
		if validFrontierSourceID(value) {
			t.Errorf("invalid source ID %q accepted", value)
		}
	}
}

func TestFrontierHeaderValidation(t *testing.T) {
	for _, input := range []string{
		"not-json\n",
		"{\"type\":\"wrong\",\"version\":1,\"sourceId\":\"source\"}\n",
		"{\"type\":\"yacy-queue-frontier\",\"version\":2,\"sourceId\":\"source\"}\n",
		"{\"type\":\"yacy-queue-frontier\",\"version\":1,\"sourceId\":\"bad/id\"}\n",
	} {
		if _, _, err := readFrontierHeader(bufio.NewReader(strings.NewReader(input))); err == nil {
			t.Errorf("invalid frontier header %q accepted", input)
		}
	}
	headerLine := mustJSONRaw(
		t,
		frontierFileHeader{Type: "yacy-queue-frontier", Version: 1, SourceID: "source"},
	)
	if header, offset, err := readFrontierHeader(
		bufio.NewReader(bytes.NewReader(headerLine)),
	); err != nil ||
		header.SourceID != "source" ||
		offset != int64(len(headerLine)) {
		t.Fatalf("unterminated header = %+v, %d, %v", header, offset, err)
	}
	if _, _, err := readFrontierHeader(
		bufio.NewReader(
			strings.NewReader(strings.Repeat("x", yagocrawlcontract.MaximumCrawlURLBytes*2)),
		),
	); err == nil {
		t.Fatal("oversized header was accepted")
	}
}

func TestYaCyProfileSidecarValidation(t *testing.T) {
	if record, done, err := readYaCyProfileRecord(
		bufio.NewReader(strings.NewReader("")),
	); err != nil ||
		!done ||
		record.Handle != "" {
		t.Fatalf("empty profile stream = %+v, %t, %v", record, done, err)
	}
	for _, input := range []string{
		"not-json\n",
		"{\"type\":\"wrong\",\"version\":1,\"handle\":\"p\",\"fields\":{\"depth\":\"1\"}}\n",
		"{\"type\":\"yacy-crawl-profile\",\"version\":2,\"handle\":\"p\",\"fields\":{\"depth\":\"1\"}}\n",
		"{\"type\":\"yacy-crawl-profile\",\"version\":1,\"handle\":\"\",\"fields\":{\"depth\":\"1\"}}\n",
		"{\"type\":\"yacy-crawl-profile\",\"version\":1,\"handle\":\"p\",\"fields\":{}}\n",
	} {
		if _, _, err := readYaCyProfileRecord(
			bufio.NewReader(strings.NewReader(input)),
		); err == nil {
			t.Errorf("invalid profile row %q accepted", input)
		}
	}
	if _, _, err := readYaCyProfileRecord(
		bufio.NewReader(strings.NewReader(strings.Repeat("x", 20<<20))),
	); err == nil {
		t.Fatal("oversized profile row was accepted")
	}
	if err := validateEmptyProfileSidecar(t); err == nil {
		t.Fatal("empty profile sidecar was accepted")
	}
	duplicate := yaCyProfileRecord{
		Type: "yacy-crawl-profile", Version: 1, State: "active", Handle: "p",
		Fields: map[string]string{"generalDepth": "1"},
	}
	if err := validateProfileSidecarRecords(t, duplicate, duplicate); err == nil {
		t.Fatal("duplicate profile handles were accepted")
	}
	for _, header := range []frontierFileHeader{
		{Type: "wrong", Version: 1, SourceID: "source"},
		{Type: "yacy-crawl-profiles", Version: 2, SourceID: "source"},
		{Type: "yacy-crawl-profiles", Version: 1, SourceID: "bad/source"},
	} {
		content, err := json.Marshal(header)
		if err != nil {
			t.Fatal(err)
		}
		file := writeProfileContent(t, append(content, '\n'))
		if _, _, err := loadYaCyProfiles(file); err == nil {
			t.Errorf("invalid profile sidecar header %+v accepted", header)
		}
		_ = file.Close()
	}
	closed := writeProfileContent(t, []byte("{}\n"))
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadYaCyProfiles(closed); err == nil {
		t.Fatal("closed profile sidecar was accepted")
	}
}

func TestYaCyProfileLimitsAndRequestMappingAcceptAndRejectInputs(t *testing.T) {
	profile := validFrontierProfile(t)
	for _, fixture := range []struct {
		fields  map[string]string
		wantErr bool
	}{
		{fields: map[string]string{}},
		{fields: map[string]string{"generalDepth": "99", "domMaxPages": "12", "obeyHtmlRobotsNofollow": "false"}},
		{fields: map[string]string{"generalDepth": "-1"}, wantErr: true},
		{fields: map[string]string{"generalDepth": "many"}, wantErr: true},
		{fields: map[string]string{"domMaxPages": "many"}, wantErr: true},
		{fields: map[string]string{"obeyHtmlRobotsNofollow": "sometimes"}, wantErr: true},
		{fields: map[string]string{"recrawlIfOlder": "never"}, wantErr: true},
		{fields: map[string]string{"recrawlIfOlder": "-1"}, wantErr: true},
	} {
		testProfile := profile
		err := applyYaCyProfileLimits(fixture.fields, &testProfile)
		if (err != nil) != fixture.wantErr {
			t.Errorf(
				"profile fields %v error = %v, wantErr=%t",
				fixture.fields,
				err,
				fixture.wantErr,
			)
		}
	}
	if _, err := mapYaCyProfile(yaCyProfileRecord{
		Type: "yacy-crawl-profile", Version: 1, Handle: "p",
		Fields: map[string]string{strings.Repeat("k", 241): "v"},
	}); err == nil {
		t.Fatal("oversized profile key was accepted")
	}
	for _, fields := range []map[string]string{
		{"description": strings.Repeat("v", 32<<10+1)},
		{"crawlerURLMustMatch": "["},
	} {
		if _, err := mapYaCyProfile(yaCyProfileRecord{
			Type: "yacy-crawl-profile", Version: 1, Handle: "p", Fields: fields,
		}); err == nil {
			t.Errorf("invalid profile fields %v were accepted", fields)
		}
	}
	for _, row := range []yaCyFrontierRecord{
		{URL: "https://example.test/", ProfileHandle: "p", Depth: -1},
		{URL: "https://example.test/", ProfileHandle: "p", Anchor: strings.Repeat("x", yagocrawlcontract.MaximumCrawlOrderImportAnchorBytes+1)},
	} {
		if _, err := mapYaCyFrontierRequest(row, profile); err == nil {
			t.Errorf("invalid frontier request %+v accepted", row)
		}
	}
	request, err := mapYaCyFrontierRequest(yaCyFrontierRecord{
		Stack: "LOCAL", URL: "https://example.test/", ProfileHandle: "p",
		InitiatorHex:   hex.EncodeToString([]byte("AAAAAAAAAAAA")),
		QueuedAtMillis: 1_700_000_000_000,
	}, profile)
	if err != nil || request.Initiator == "" || request.AppDate.IsZero() {
		t.Fatalf("valid frontier request = %+v, %v", request, err)
	}
	for raw, want := range map[string]string{
		"https://example.test/": "", "ftp://example.test/file": "", "smb://example.test/share": "",
		"https://user:secret@example.test/": "credential", "file://example.test/file": "invalid",
		"relative/path": "invalid", "https://example.test/page#fragment": "invalid",
	} {
		if got := frontierURLBlockReason(raw); got != want {
			t.Errorf("frontierURLBlockReason(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestSendFrontierImportBatchReportsHTTPAndReceiptFailures(t *testing.T) {
	profile := validFrontierProfile(t)
	batch := makeFrontierImportBatch(
		"source", "p", map[string]frontierSourceProfile{"p": {profile: profile}},
		[]yagocrawlcontract.CrawlRequest{{URL: "https://example.test/", ProfileHandle: "p"}}, false,
	)
	for _, fixture := range []struct {
		name         string
		status       int
		body         string
		transportErr bool
		wantErr      bool
	}{
		{"success", http.StatusOK, "{\"enqueuedRequests\":1}", false, false},
		{"http failure", http.StatusBadRequest, "bad", false, true},
		{"malformed receipt", http.StatusOK, "not-json", false, true},
		{"unknown receipt field", http.StatusOK, "{\"unknown\":1}", false, true},
		{"transport failure", 0, "", true, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			client := &http.Client{
				Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.Header.Get("Authorization") != "Bearer key" {
						t.Error("frontier API key was omitted")
					}
					if fixture.transportErr {
						return nil, errors.New("transport failure")
					}
					return &http.Response{
						StatusCode: fixture.status,
						Body:       io.NopCloser(strings.NewReader(fixture.body)),
						Header:     make(http.Header),
						Request:    request,
					}, nil
				}),
			}
			_, err := sendFrontierImportBatch(
				context.Background(),
				client,
				"https://example.test/admin/crawl/frontier/import",
				"key",
				batch,
			)
			if (err != nil) != fixture.wantErr {
				t.Fatalf("send error = %v, wantErr=%t", err, fixture.wantErr)
			}
		})
	}
	if _, err := sendFrontierImportBatch(
		context.Background(),
		http.DefaultClient,
		"://invalid",
		"key",
		batch,
	); err == nil {
		t.Fatal("malformed destination was accepted")
	}
}

func TestFrontierCheckpointAndProgressFailurePaths(t *testing.T) {
	base, statePath := newFrontierCheckpointFixture(t)
	assertFrontierCheckpointVariants(t, base)
	assertFrontierCheckpointFileFailures(t, statePath)
	assertFrontierProgressWriters(t)
}

func newFrontierCheckpointFixture(t *testing.T) (frontierCheckpointValidation, string) {
	t.Helper()
	input, profilePath := writeFrontierFixtures(t, nil)
	inputFile, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, inputFile)
	profileFile, err := os.Open(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, profileFile)
	inputInfo, err := inputFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	profileInfo, err := profileFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := url.Parse("https://example.test/admin/crawl/frontier/import")
	base := frontierCheckpointValidation{
		found: true, inputInfo: inputInfo, profileInfo: profileInfo, endpoint: endpoint,
		inputHash: "input", profilesHash: "profiles", sourceID: "fixture-source",
		state: frontierImportCheckpoint{
			Version:                1,
			SourceID:               "fixture-source",
			InputSHA256:            "input",
			ProfilesSHA256:         "profiles",
			InputSize:              inputInfo.Size(),
			InputModTime:           inputInfo.ModTime().UnixNano(),
			ProfilesSize:           profileInfo.Size(),
			ProfilesModTime:        profileInfo.ModTime().UnixNano(),
			DestinationFingerprint: destinationFingerprint(endpoint),
		},
	}
	return base, filepath.Join(t.TempDir(), "state.json")
}

func assertFrontierCheckpointVariants(t *testing.T, base frontierCheckpointValidation) {
	t.Helper()
	if err := validateFrontierCheckpoint(base); err != nil {
		t.Fatalf("valid checkpoint rejected: %v", err)
	}
	for _, edit := range []func(*frontierCheckpointValidation){
		func(c *frontierCheckpointValidation) { c.state.Version++ },
		func(c *frontierCheckpointValidation) { c.state.SourceID = "other" },
		func(c *frontierCheckpointValidation) { c.state.InputSHA256 = "other" },
		func(c *frontierCheckpointValidation) { c.state.ProfilesSHA256 = "other" },
		func(c *frontierCheckpointValidation) { c.state.InputSize++ },
		func(c *frontierCheckpointValidation) { c.state.InputModTime++ },
		func(c *frontierCheckpointValidation) { c.state.ProfilesSize++ },
		func(c *frontierCheckpointValidation) { c.state.ProfilesModTime++ },
		func(c *frontierCheckpointValidation) { c.state.Offset = -1 },
		func(c *frontierCheckpointValidation) { c.state.Offset = base.state.InputSize + 1 },
		func(c *frontierCheckpointValidation) { c.endpoint = nil },
		func(c *frontierCheckpointValidation) { c.state.DestinationFingerprint = "other" },
	} {
		check := base
		edit(&check)
		if err := validateFrontierCheckpoint(check); err == nil {
			t.Error("mismatched checkpoint was accepted")
		}
	}
	base.dryRun = true
	if err := validateFrontierCheckpoint(base); err != nil {
		t.Fatalf("dry-run checkpoint: %v", err)
	}
	base.dryRun, base.found = false, false
	if err := validateFrontierCheckpoint(base); err != nil {
		t.Fatalf("new checkpoint: %v", err)
	}
}

func assertFrontierCheckpointFileFailures(t *testing.T, statePath string) {
	t.Helper()
	if _, found, err := loadFrontierImportCheckpoint(statePath); err != nil || found {
		t.Fatalf("missing checkpoint = found %t, err %v", found, err)
	}
	if err := os.WriteFile(statePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadFrontierImportCheckpoint(statePath); err == nil {
		t.Fatal("malformed checkpoint was accepted")
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadFrontierImportCheckpoint(statePath); err == nil {
		t.Fatal("directory was accepted as a checkpoint")
	}
}

func assertFrontierProgressWriters(t *testing.T) {
	t.Helper()
	if err := writeFrontierImportProgress(errorWriter{}, frontierImportCheckpoint{}); err == nil {
		t.Fatal("progress write error was ignored")
	}
	if err := writeFrontierImportSummary(
		errorWriter{},
		frontierImportCheckpoint{},
		true,
	); err == nil {
		t.Fatal("summary write error was ignored")
	}
	if err := writeFrontierImportSummary(
		io.Discard,
		frontierImportCheckpoint{},
		false,
	); err != nil {
		t.Fatalf("progress summary: %v", err)
	}
}

func TestHashOpenFileReportsSeekAndReadFailures(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		reader frontierHashReader
	}{
		{name: "initial seek", reader: frontierHashReader{failSeekAt: 1}},
		{name: "read", reader: frontierHashReader{readErr: true}},
		{name: "rewind", reader: frontierHashReader{failSeekAt: 2}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, err := hashOpenFile(&fixture.reader); err == nil {
				t.Fatal("hash operation failure was ignored")
			}
		})
	}
}

func TestFrontierImportRejectsOpenAndFingerprintFailures(t *testing.T) {
	input, profiles := writeFrontierFixtures(t, nil)
	assertFrontierImportRejectsInvalidPaths(t, input, profiles)
	assertFrontierSourceInspectionRejectsClosedFiles(t, input, profiles)
}

func assertFrontierImportRejectsInvalidPaths(t *testing.T, input, profiles string) {
	t.Helper()
	base := frontierImportOptions{
		Input: input, Profiles: profiles, State: filepath.Join(t.TempDir(), "state.json"),
		BatchRecords: 1, DryRun: true,
	}
	invalidOptions := base
	invalidOptions.BatchRecords = 0
	if err := importFrontier(context.Background(), invalidOptions, io.Discard); err == nil {
		t.Fatal("invalid frontier options were accepted")
	}
	missingInput := base
	missingInput.Input = filepath.Join(t.TempDir(), "missing-frontier")
	if err := importFrontier(context.Background(), missingInput, io.Discard); err == nil {
		t.Fatal("missing frontier input was accepted")
	}
	missingProfiles := base
	missingProfiles.Profiles = filepath.Join(t.TempDir(), "missing-profiles")
	if err := importFrontier(context.Background(), missingProfiles, io.Discard); err == nil {
		t.Fatal("missing profile sidecar was accepted")
	}
	directory := t.TempDir()
	profileDirectory := base
	profileDirectory.Profiles = directory
	if err := importFrontier(context.Background(), profileDirectory, io.Discard); err == nil {
		t.Fatal("directory was accepted as a profile sidecar")
	}
	invalidProfiles := base
	invalidProfiles.Profiles = filepath.Join(t.TempDir(), "invalid-profiles.jsonl")
	if err := os.WriteFile(invalidProfiles.Profiles, []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := importFrontier(context.Background(), invalidProfiles, io.Discard); err == nil {
		t.Fatal("malformed profile sidecar was accepted")
	}
	for _, profileRecord := range []string{
		"not-json",
		`{"type":"yacy-crawl-profile","version":1,"handle":"p","fields":{"crawlerURLMustMatch":"["}}`,
	} {
		invalidProfiles.Profiles = filepath.Join(t.TempDir(), "invalid-profile-row.jsonl")
		content := append(frontierProfileHeaderLine(t), []byte(profileRecord+"\n")...)
		if err := os.WriteFile(invalidProfiles.Profiles, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := importFrontier(context.Background(), invalidProfiles, io.Discard); err == nil {
			t.Errorf("invalid profile row %q was accepted", profileRecord)
		}
	}
	inputDirectory := base
	inputDirectory.Input = directory
	if err := importFrontier(context.Background(), inputDirectory, io.Discard); err == nil {
		t.Fatal("directory was accepted as a frontier file")
	}
}

func assertFrontierSourceInspectionRejectsClosedFiles(t *testing.T, input, profiles string) {
	t.Helper()
	inputFile, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, inputFile)
	profileFile, err := os.Open(profiles)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, profileFile)
	if err := inputFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFrontierSourceData(inputFile, profileFile); err == nil {
		t.Fatal("closed frontier input passed source inspection")
	}
	if err := profileFile.Close(); err != nil {
		t.Fatal(err)
	}
	inputFile, err = os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, inputFile)
	if _, err := loadFrontierSourceData(inputFile, profileFile); err == nil {
		t.Fatal("closed profile sidecar passed source inspection")
	}
}

func TestPrepareFrontierSessionRejectsHeaderCheckpointAndSourceMismatch(t *testing.T) {
	input, profiles := writeFrontierFixtures(t, nil)
	inputFile, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, inputFile)
	profileFile, err := os.Open(profiles)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, profileFile)
	source, err := loadFrontierSourceData(inputFile, profileFile)
	if err != nil {
		t.Fatal(err)
	}
	options := frontierImportOptions{
		Input: input, Profiles: profiles, Endpoint: "not-an-endpoint",
		State: filepath.Join(t.TempDir(), "state.json"), BatchRecords: 1, DryRun: true,
	}
	if _, err := prepareFrontierSession(inputFile, options, source); err == nil {
		t.Fatal("invalid endpoint was accepted by session preparation")
	}
	options.Endpoint = ""
	options.State = t.TempDir()
	if _, err := prepareFrontierSession(inputFile, options, source); err == nil {
		t.Fatal("directory was accepted as a checkpoint file")
	}
	options.State = filepath.Join(t.TempDir(), "missing", "state.json")
	options.DryRun = false
	options.Endpoint = "https://example.test/admin/crawl/frontier/import"
	options.APIKey = "key"
	if _, err := prepareFrontierSession(inputFile, options, source); err == nil {
		t.Fatal("unwritable checkpoint path was accepted")
	}
	assertFrontierSessionRejectsMalformedRecovery(
		t,
		inputFile,
		profileFile,
		options,
	)
}

func assertFrontierSessionRejectsMalformedRecovery(
	t *testing.T,
	inputFile, profileFile *os.File,
	options frontierImportOptions,
) {
	t.Helper()
	sourceInput, sourceProfiles := options.Input, options.Profiles
	assertFrontierSessionRejectsMalformedHeader(t, profileFile, options)
	assertFrontierSessionRejectsSavedPrefix(t, profileFile, options)
	assertFrontierSessionRejectsProfileMismatch(
		t,
		inputFile,
		options,
		sourceInput,
		sourceProfiles,
	)
}

func assertFrontierSessionRejectsMalformedHeader(
	t *testing.T,
	profileFile *os.File,
	options frontierImportOptions,
) {
	t.Helper()
	badInput := filepath.Join(t.TempDir(), "bad-frontier.jsonl")
	if err := os.WriteFile(badInput, []byte("invalid header\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	badFile, err := os.Open(badInput)
	if err != nil {
		t.Fatal(err)
	}
	badSource, err := loadFrontierSourceData(badFile, profileFile)
	if err != nil {
		t.Fatal(err)
	}
	options.Input, options.DryRun = badInput, true
	options.State = filepath.Join(t.TempDir(), "bad-header.state")
	if _, err := prepareFrontierSession(badFile, options, badSource); err == nil {
		t.Fatal("invalid frontier header was accepted")
	}
	if err := badFile.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertFrontierSessionRejectsSavedPrefix(
	t *testing.T,
	profileFile *os.File,
	options frontierImportOptions,
) {
	t.Helper()
	restoreInput := filepath.Join(t.TempDir(), "restore-frontier.jsonl")
	header, err := json.Marshal(frontierFileHeader{
		Type: "yacy-queue-frontier", Version: 1, SourceID: "fixture-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	restoreBytes := append(append(header, '\n'), []byte("bad\n")...)
	if err := os.WriteFile(restoreInput, restoreBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	restoreFile, err := os.Open(restoreInput)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, restoreFile)
	restoreSource, err := loadFrontierSourceData(restoreFile, profileFile)
	if err != nil {
		t.Fatal(err)
	}
	state, err := readFrontierRecoveryCheckpoint(
		t,
		restoreFile,
		restoreSource,
		options.Endpoint,
		int64(len(restoreBytes)),
	)
	if err != nil {
		t.Fatal(err)
	}
	options.Input = restoreInput
	options.State = filepath.Join(t.TempDir(), "restore.state")
	options.DryRun = false
	if err := saveCheckpoint(options.State, state); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareFrontierSession(restoreFile, options, restoreSource); err == nil {
		t.Fatal("invalid saved prefix was accepted during session recovery")
	}
}

func assertFrontierSessionRejectsProfileMismatch(
	t *testing.T,
	inputFile *os.File,
	options frontierImportOptions,
	sourceInput, sourceProfiles string,
) {
	t.Helper()
	options.DryRun = true
	profileBytes, err := os.ReadFile(options.Profiles)
	if err != nil {
		t.Fatal(err)
	}
	profileBytes = []byte(
		strings.Replace(string(profileBytes), "fixture-source", "other-source", 1),
	)
	if err := os.WriteFile(options.Profiles, profileBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	profileMismatch, err := os.Open(options.Profiles)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, profileMismatch)
	mismatchedSource, err := loadFrontierSourceData(inputFile, profileMismatch)
	if err != nil {
		t.Fatal(err)
	}
	options.Input, options.Profiles = sourceInput, sourceProfiles
	if _, err := prepareFrontierSession(inputFile, options, mismatchedSource); err == nil {
		t.Fatal("frontier/profile source mismatch was accepted")
	}
}

func readFrontierRecoveryCheckpoint(
	t *testing.T,
	input *os.File,
	source frontierSourceData,
	endpoint string,
	offset int64,
) (frontierImportCheckpoint, error) {
	t.Helper()
	parsed, err := validateFrontierEndpoint(endpoint, false)
	if err != nil {
		return frontierImportCheckpoint{}, err
	}
	reader := bufio.NewReader(input)
	header, headerEnd, err := readFrontierHeader(reader)
	if err != nil {
		return frontierImportCheckpoint{}, err
	}
	state := newFrontierCheckpoint(source, header.SourceID, headerEnd)
	state.Offset = offset
	state.DestinationFingerprint = destinationFingerprint(parsed)
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return frontierImportCheckpoint{}, fmt.Errorf("rewind frontier input: %w", err)
	}
	return state, nil
}

type frontierSeenValidationFixture struct {
	profiles   map[string]frontierSourceProfile
	row        yaCyFrontierRecord
	headerLine []byte
	validData  []byte
}

func newFrontierSeenValidationFixture(t *testing.T) frontierSeenValidationFixture {
	t.Helper()
	profile, err := mapYaCyProfile(yaCyProfileRecord{
		Type: "yacy-crawl-profile", Version: 1, Handle: "source-profile",
		Fields: map[string]string{"generalDepth": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	profiles := map[string]frontierSourceProfile{
		"source-profile": {sourceHandle: "source-profile", profile: profile},
	}
	row := yaCyFrontierRecord{
		Type: "crawlRequest", Version: 1, Stack: "LOCAL",
		URL: "https://example.test/one", ProfileHandle: "source-profile",
	}
	rowLine, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	header, err := json.Marshal(frontierFileHeader{
		Type: "yacy-queue-frontier", Version: 1, SourceID: "fixture-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	headerLine := append([]byte(nil), header...)
	headerLine = append(headerLine, '\n')
	validLine := append([]byte(nil), rowLine...)
	validLine = append(validLine, '\n')
	validData := append(append([]byte(nil), headerLine...), validLine...)
	return frontierSeenValidationFixture{
		profiles: profiles, row: row, headerLine: headerLine, validData: validData,
	}
}

func TestFrontierSeenRecoveryRejectsInvalidPrefixes(t *testing.T) {
	fixture := newFrontierSeenValidationFixture(t)
	remoteRow := fixture.row
	remoteRow.Stack = "REMOTE"
	remoteLine := mustJSONRaw(t, remoteRow)
	remoteLine = append(remoteLine, '\n')
	malformedData := append(append([]byte(nil), fixture.headerLine...), []byte("bad\n")...)
	remoteData := append(append([]byte(nil), fixture.headerLine...), remoteLine...)
	for _, fixture := range []struct {
		name     string
		data     []byte
		end      int64
		profiles map[string]frontierSourceProfile
	}{
		{name: "invalid header", data: []byte("invalid\n"), end: 0, profiles: fixture.profiles},
		{name: "before header boundary", data: fixture.validData, end: int64(len(fixture.headerLine) - 1), profiles: fixture.profiles},
		{name: "past record boundary", data: fixture.validData, end: int64(len(fixture.headerLine) + 1), profiles: fixture.profiles},
		{name: "past available data", data: fixture.validData, end: int64(len(fixture.validData) + 4), profiles: fixture.profiles},
		{name: "malformed record", data: malformedData, end: int64(len(malformedData)), profiles: fixture.profiles},
		{name: "unknown profile", data: fixture.validData, end: int64(len(fixture.validData)), profiles: map[string]frontierSourceProfile{}},
		{
			name: "nonlocal record", data: remoteData,
			end: int64(len(remoteData)), profiles: fixture.profiles,
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			file := writeProfileContent(t, fixture.data)
			cleanupFrontierFile(t, file)
			if _, err := rebuildFrontierSeen(file, fixture.end, fixture.profiles); err == nil {
				t.Fatal("invalid checkpoint prefix was accepted")
			}
		})
	}
}

func TestFrontierSeenRecoveryRestoresRowsAndEnforcesLimits(t *testing.T) {
	fixture := newFrontierSeenValidationFixture(t)
	file := writeProfileContent(t, fixture.validData)
	cleanupFrontierFile(t, file)
	seen, err := rebuildFrontierSeen(file, int64(len(fixture.validData)), fixture.profiles)
	if err != nil || len(seen) != 1 {
		t.Fatalf("valid frontier prefix = %d seen rows, %v", len(seen), err)
	}
	secondRow := fixture.row
	secondRow.URL = "https://example.test/two"
	secondLine, err := json.Marshal(secondRow)
	if err != nil {
		t.Fatal(err)
	}
	uniqueRows := append(append([]byte(nil), fixture.validData...), secondLine...)
	uniqueRows = append(uniqueRows, '\n')
	limitedFile := writeProfileContent(t, uniqueRows)
	cleanupFrontierFile(t, limitedFile)
	if _, err := rebuildFrontierSeenWithLimit(
		limitedFile, int64(len(uniqueRows)), fixture.profiles, 1,
	); err == nil {
		t.Fatal("recovered frontier exceeded its seen-URL bound")
	}
	closedFile := writeProfileContent(t, fixture.validData)
	if err := closedFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rebuildFrontierSeen(
		closedFile,
		int64(len(fixture.validData)),
		fixture.profiles,
	); err == nil {
		t.Fatal("closed frontier input was accepted during recovery")
	}
	oversizedData := append(append([]byte(nil), fixture.headerLine...),
		bytes.Repeat([]byte("x"), yagocrawlcontract.MaximumCrawlOrderImportBatchBytes*16+1)...)
	oversizedFile := writeProfileContent(t, oversizedData)
	cleanupFrontierFile(t, oversizedFile)
	if _, err := rebuildFrontierSeen(
		oversizedFile, int64(len(oversizedData)), fixture.profiles,
	); err == nil {
		t.Fatal("oversized saved frontier record was accepted")
	}
}

func TestFrontierSeenTrackingDeduplicatesAndEnforcesLimits(t *testing.T) {
	seenRows := make(map[string]struct{})
	if duplicate, err := addFrontierSeen(seenRows, "profile\x00url", 1); err != nil || duplicate {
		t.Fatalf("first identity = duplicate %t, err %v", duplicate, err)
	}
	if duplicate, err := addFrontierSeen(seenRows, "profile\x00url", 1); err != nil || !duplicate {
		t.Fatalf("repeated identity = duplicate %t, err %v", duplicate, err)
	}
	if _, err := addFrontierSeen(seenRows, "profile\x00other", 1); err == nil {
		t.Fatal("frontier seen limit was not enforced")
	}
	run := newFrontierTestRun(t, true, 1)
	run.maximumSeen = 0
	if duplicate, err := run.recordSeen(
		validFrontierRow("p", "https://example.test/"),
	); err != nil ||
		duplicate {
		t.Fatalf("default seen limit = duplicate %t, err %v", duplicate, err)
	}
}

func TestSeekFrontierImportInputReportsSuccessAndFailure(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "frontier")
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, file)
	if err := seekFrontierImportInput(file, 0); err != nil {
		t.Fatalf("seek regular frontier: %v", err)
	}
	if err := seekFrontierImportInput(frontierSeekFailure{}, 0); err == nil {
		t.Fatal("seek failure was ignored")
	}
}

func TestPrepareFrontierSessionReportsNonseekableInput(t *testing.T) {
	inputPath, profilesPath := writeFrontierFixtures(t, nil)
	inputFile, err := os.Open(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, inputFile)
	profileFile, err := os.Open(profilesPath)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, profileFile)
	source, err := loadFrontierSourceData(inputFile, profileFile)
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(frontierHeaderLine(t, "fixture-source")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, reader)
	_, err = prepareFrontierSession(reader, frontierImportOptions{
		Input: inputPath, Profiles: profilesPath,
		Endpoint: "https://example.test/admin/crawl/frontier/import",
		State:    filepath.Join(t.TempDir(), "state.json"), BatchRecords: 1, DryRun: true,
	}, source)
	if err == nil {
		t.Fatal("nonseekable frontier input was accepted")
	}
}

func frontierHeaderLine(t *testing.T, sourceID string) []byte {
	t.Helper()
	line, err := json.Marshal(frontierFileHeader{
		Type: "yacy-queue-frontier", Version: 1, SourceID: sourceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(line, '\n')
}

func TestReadFrontierStreamsReportEmptyAndFilesystemReadErrors(t *testing.T) {
	if _, _, err := readFrontierHeader(bufio.NewReader(strings.NewReader(""))); err == nil {
		t.Fatal("empty frontier header was accepted")
	}
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, directory)
	if _, _, err := readFrontierHeader(bufio.NewReader(directory)); err == nil {
		t.Fatal("directory read as a frontier header")
	}
	directory, err = os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, directory)
	if _, _, err := loadYaCyProfiles(directory); err == nil {
		t.Fatal("directory read as a profile sidecar")
	}
	directory, err = os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, directory)
	if _, _, err := readYaCyProfileRecord(bufio.NewReader(directory)); err == nil {
		t.Fatal("directory read as a profile record")
	}
}

func TestFrontierURLValidationRejectsLengthAndInvalidUTF8(t *testing.T) {
	if got := frontierURLBlockReason(string([]byte{0xff})); got != "invalid" {
		t.Fatalf("invalid UTF-8 URL reason = %q", got)
	}
	if got := frontierURLBlockReason(
		"https://example.test/" + strings.Repeat("x", yagocrawlcontract.MaximumCrawlURLBytes),
	); got != "invalid" {
		t.Fatalf("oversized URL reason = %q", got)
	}
}

func TestFrontierBatchRunCoversProfilesDuplicatesAndValidation(t *testing.T) {
	run := newFrontierTestRun(t, true, 2)
	second := validFrontierProfile(t)
	second.Handle = "second"
	run.session.profiles[second.Handle] = frontierSourceProfile{
		sourceHandle: second.Handle, profile: second,
	}
	firstRow := validFrontierRow("p", "https://example.test/first")
	secondRow := validFrontierRow("second", "https://example.test/second")
	if err := run.addRow(firstRow, 10); err != nil {
		t.Fatal(err)
	}
	if err := run.addRow(secondRow, 20); err != nil {
		t.Fatalf("profile transition flush: %v", err)
	}
	if run.profileHandle != "second" || len(run.batch) != 1 {
		t.Fatalf("new profile batch = %q, %d requests", run.profileHandle, len(run.batch))
	}
	if err := run.addRow(secondRow, 30); err != nil {
		t.Fatalf("duplicate request: %v", err)
	}
	if run.duplicates != 1 {
		t.Fatalf("duplicate count = %d", run.duplicates)
	}
	if err := run.addRow(
		validFrontierRow("second", "https://user:secret@example.test/"),
		40,
	); err != nil {
		t.Fatal(err)
	}
	if err := run.addRow(
		validFrontierRow("second", "https://user:secret@example.test/"),
		50,
	); err != nil {
		t.Fatal(err)
	}
	if err := run.addRow(validFrontierRow("second", "file://example.test/path"), 60); err != nil {
		t.Fatal(err)
	}
	if run.blockedCreds != 1 || run.blockedInvalid != 1 || run.duplicates != 2 {
		t.Fatalf(
			"blocked and duplicate counters = %d, %d, %d",
			run.blockedCreds,
			run.blockedInvalid,
			run.duplicates,
		)
	}
	if err := run.addRequestRow(yaCyFrontierRecord{
		URL:           "https://example.test/depth",
		ProfileHandle: "second",
		Depth:         yagocrawlcontract.MaxCrawlDepth + 1,
	}, 70); err == nil {
		t.Fatal("invalid request depth was accepted")
	}
	limited := newFrontierTestRun(t, true, 1)
	limited.maximumSeen = 1
	limited.session.seen["other\x00url"] = struct{}{}
	if err := limited.addBlockedRow(
		validFrontierRow("p", "https://user@example.test/"),
		"credential",
	); err == nil {
		t.Fatal("blocked row exceeded the deduplication limit")
	}
	if err := limited.addRequestRow(
		validFrontierRow("p", "https://example.test/"),
		80,
	); err == nil {
		t.Fatal("request row exceeded the deduplication limit")
	}
}

func TestFrontierBatchRunReturnsFlushErrorsDuringTransitions(t *testing.T) {
	run := newFrontierTestRun(t, true, 2)
	run.session.state.SourceID = "bad/id"
	run.batch = []yagocrawlcontract.CrawlRequest{{
		URL: "https://example.test/first", ProfileHandle: "p",
	}}
	run.batchRows, run.profileHandle = 1, "p"
	second := validFrontierProfile(t)
	second.Handle = "second"
	run.session.profiles[second.Handle] = frontierSourceProfile{
		sourceHandle: second.Handle, profile: second,
	}
	if err := run.addRow(
		validFrontierRow("second", "https://example.test/second"),
		20,
	); err == nil {
		t.Fatal("profile transition ignored an invalid pending batch")
	}
	if err := run.flush(); err == nil {
		t.Fatal("flush ignored an invalid mapped batch")
	}
}

func TestFrontierBatchRunReadAndExecuteFailurePaths(t *testing.T) {
	run := newFrontierTestRun(t, true, 1)
	run.session.reader = bufio.NewReader(frontierReadFailure{})
	if _, _, _, err := run.readRow(); err == nil {
		t.Fatal("frontier read failure was ignored")
	}
	run.session.reader = bufio.NewReader(strings.NewReader("not-json\n"))
	if _, _, _, err := run.readRow(); err == nil {
		t.Fatal("malformed frontier row was accepted")
	}
	unknownProfile, err := json.Marshal(validFrontierRow("missing", "https://example.test/"))
	if err != nil {
		t.Fatal(err)
	}
	run.session.reader = bufio.NewReader(bytes.NewReader(append(unknownProfile, '\n')))
	if _, _, _, err := run.readRow(); err == nil {
		t.Fatal("frontier row with a missing profile was accepted")
	}
	run.session.reader = bufio.NewReader(strings.NewReader(""))
	if _, _, done, err := run.readRow(); err != nil || !done {
		t.Fatalf("empty frontier read = done %t, err %v", done, err)
	}
	run.session.reader = bufio.NewReader(
		bytes.NewReader(frontierRowLines(t, validFrontierRow("p", "https://example.test/"))),
	)
	if err := run.execute(); err != nil {
		t.Fatalf("dry-run execution: %v", err)
	}
	badRequest := validFrontierRow("p", "https://example.test/")
	badRequest.Depth = yagocrawlcontract.MaxCrawlDepth + 1
	run = newFrontierTestRun(t, true, 1)
	run.session.reader = bufio.NewReader(bytes.NewReader(frontierRowLines(t, badRequest)))
	if err := run.execute(); err == nil {
		t.Fatal("invalid mapped frontier request was accepted")
	}
	deferredFlush := newFrontierTestRun(t, false, 2)
	deferredFlush.opts.APIKey = "key"
	deferredFlush.session.endpoint, _ = url.Parse(
		"https://example.test/admin/crawl/frontier/import",
	)
	deferredFlush.session.reader = bufio.NewReader(bytes.NewReader(frontierRowLines(
		t, validFrontierRow("p", "https://example.test/"),
	)))
	if err := deferredFlush.session.input.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if err := deferredFlush.execute(); err == nil {
		t.Fatal("execute ignored a deferred source-verification failure")
	}
	noCheckpoint := newFrontierTestRun(t, false, 1)
	noCheckpoint.opts.MaxRecords = 1
	noCheckpoint.session.statePath = filepath.Join(t.TempDir(), "missing", "state.json")
	noCheckpoint.session.reader = bufio.NewReader(strings.NewReader(""))
	if err := noCheckpoint.execute(); err == nil {
		t.Fatal("checkpoint persistence failure was ignored")
	}
	if err := newFrontierTestRun(t, true, 1).execute(); err != nil {
		t.Fatalf("empty dry run: %v", err)
	}
	writerFailure := newFrontierTestRun(t, true, 1)
	writerFailure.progress = errorWriter{}
	writerFailure.session.reader = bufio.NewReader(strings.NewReader(""))
	if err := writerFailure.execute(); err == nil {
		t.Fatal("summary writer failure was ignored")
	}
}

func TestFrontierBatchFlushRejectsMutationHTTPAndCheckpointFailures(t *testing.T) {
	for _, fixture := range []struct {
		name         string
		mutate       func(*frontierBatchRun)
		serverStatus int
		wantErr      bool
	}{
		{
			name: "mutated source", mutate: func(run *frontierBatchRun) {
				if err := run.session.input.Truncate(0); err != nil {
					t.Fatal(err)
				}
			}, wantErr: true,
		},
		{name: "endpoint failure", serverStatus: http.StatusBadRequest, wantErr: true},
		{
			name: "checkpoint failure", serverStatus: http.StatusOK, wantErr: true,
		},
		{
			name: "progress failure", serverStatus: http.StatusOK,
			wantErr: true,
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if fixture.serverStatus != http.StatusOK {
						w.WriteHeader(fixture.serverStatus)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"enqueuedRequests":1}`)
				}),
			)
			t.Cleanup(server.Close)
			run := newFrontierTestRun(t, false, 2)
			run.session.endpoint, _ = url.Parse(server.URL + "/admin/crawl/frontier/import")
			run.opts.APIKey = "key"
			run.session.state.SourceID = "fixture-source"
			run.session.statePath = filepath.Join(t.TempDir(), "state.json")
			run.batch = []yagocrawlcontract.CrawlRequest{{
				URL: "https://example.test/", Mode: yagocrawlcontract.CrawlRequestModeURL,
				ProfileHandle: "p",
			}}
			run.batchRows, run.profileHandle, run.batchEnd = 1, "p", 10
			if fixture.name == "checkpoint failure" {
				run.session.statePath = filepath.Join(t.TempDir(), "missing", "state.json")
			}
			if fixture.name == "progress failure" {
				run.progress = errorWriter{}
			}
			if fixture.mutate != nil {
				fixture.mutate(run)
			}
			err := run.flush()
			if (err != nil) != fixture.wantErr {
				t.Fatalf("flush error = %v, wantErr=%t", err, fixture.wantErr)
			}
		})
	}
}

func TestFrontierMappedBatchAcceptsAndRejectsBoundaryBatches(t *testing.T) {
	run := newFrontierTestRun(t, true, 256)
	if err := run.flush(); err != nil {
		t.Fatalf("empty flush: %v", err)
	}
	if _, err := run.mappedBatch(); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	request := yagocrawlcontract.CrawlRequest{
		URL: "https://example.test/one", Mode: yagocrawlcontract.CrawlRequestModeURL,
		ProfileHandle: "p",
	}
	run.batch, run.profileHandle = []yagocrawlcontract.CrawlRequest{request}, "p"
	if _, err := run.mappedBatch(); err != nil {
		t.Fatalf("valid batch: %v", err)
	}
	run.session.state.SourceID = "bad/id"
	if _, err := run.mappedBatch(); err == nil {
		t.Fatal("invalid batch source ID was accepted")
	}
	run.session.state.SourceID = "fixture-source"
	run.batch = make(
		[]yagocrawlcontract.CrawlRequest,
		yagocrawlcontract.MaximumCrawlOrderImportBatchRecords,
	)
	for index := range run.batch {
		run.batch[index] = yagocrawlcontract.CrawlRequest{
			URL:  fmt.Sprintf("https://example.test/%d", index),
			Mode: yagocrawlcontract.CrawlRequestModeURL, ProfileHandle: "p",
			AnchorName: strings.Repeat("a", yagocrawlcontract.MaximumCrawlOrderImportAnchorBytes),
		}
	}
	if _, err := run.mappedBatch(); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized mapped batch error = %v", err)
	}
}

func newFrontierTestRun(t *testing.T, dryRun bool, batchSize int) *frontierBatchRun {
	t.Helper()
	inputPath, _ := writeFrontierFixtures(t, nil)
	input, err := os.OpenFile(inputPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cleanupFrontierFile(t, input)
	info, err := input.Stat()
	if err != nil {
		t.Fatal(err)
	}
	profile := validFrontierProfile(t)
	profile.Handle = "p"
	return newFrontierBatchRun(context.Background(), frontierImportOptions{
		Input: inputPath, BatchRecords: batchSize, DryRun: dryRun,
	}, io.Discard, &frontierImportSession{
		input:     input,
		inputInfo: info,
		reader:    bufio.NewReader(strings.NewReader("")),
		profiles: map[string]frontierSourceProfile{
			profile.Handle: {sourceHandle: profile.Handle, profile: profile},
		},
		seen: make(map[string]struct{}),
		endpoint: &url.URL{
			Scheme: "http",
			Host:   "example.test",
			Path:   "/admin/crawl/frontier/import",
		},
		statePath: filepath.Join(t.TempDir(), "state.json"),
		state:     frontierImportCheckpoint{Version: 1, SourceID: "fixture-source"},
	})
}

func validFrontierRow(profile, rawURL string) yaCyFrontierRecord {
	return yaCyFrontierRecord{
		Type: "crawlRequest", Version: 1, Stack: "LOCAL", URL: rawURL,
		ProfileHandle: profile,
	}
}

func frontierRowLines(t *testing.T, rows ...yaCyFrontierRecord) []byte {
	t.Helper()
	var output bytes.Buffer
	for _, row := range rows {
		if err := json.NewEncoder(&output).Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	return output.Bytes()
}

type frontierReadFailure struct{}

func (frontierReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("read failure")
}

type frontierHashReader struct {
	reader     *strings.Reader
	failSeekAt int
	seekCalls  int
	readErr    bool
}

type frontierSeekFailure struct{}

func (frontierSeekFailure) Seek(int64, int) (int64, error) {
	return 0, errors.New("seek failure")
}

func (r *frontierHashReader) Read(buffer []byte) (int, error) {
	if r.readErr {
		return 0, errors.New("read failure")
	}
	if r.reader == nil {
		r.reader = strings.NewReader("fixture")
	}
	n, err := r.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read hash fixture: %w", err)
	}
	return n, nil
}

func (r *frontierHashReader) Seek(offset int64, whence int) (int64, error) {
	r.seekCalls++
	if r.seekCalls == r.failSeekAt {
		return 0, errors.New("seek failure")
	}
	if r.reader == nil {
		r.reader = strings.NewReader("fixture")
	}
	position, err := r.reader.Seek(offset, whence)
	if err != nil {
		return position, fmt.Errorf("seek hash fixture: %w", err)
	}
	return position, nil
}

func validateEmptyProfileSidecar(t *testing.T) error {
	t.Helper()
	header, _ := json.Marshal(
		frontierFileHeader{Type: "yacy-crawl-profiles", Version: 1, SourceID: "source"},
	)
	file := writeProfileContent(t, append(header, '\n'))
	cleanupFrontierFile(t, file)
	_, _, err := loadYaCyProfiles(file)
	return err
}

func validateProfileSidecarRecords(t *testing.T, records ...yaCyProfileRecord) error {
	t.Helper()
	header, _ := json.Marshal(
		frontierFileHeader{Type: "yacy-crawl-profiles", Version: 1, SourceID: "source"},
	)
	var content bytes.Buffer
	content.Write(header)
	content.WriteByte('\n')
	encoder := json.NewEncoder(&content)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	file := writeProfileContent(t, content.Bytes())
	cleanupFrontierFile(t, file)
	_, _, err := loadYaCyProfiles(file)
	return err
}

func frontierProfileHeaderLine(t *testing.T) []byte {
	t.Helper()
	line, err := json.Marshal(frontierFileHeader{
		Type: "yacy-crawl-profiles", Version: 1, SourceID: "fixture-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(line, '\n')
}

func cleanupFrontierFile(t *testing.T, file *os.File) {
	t.Helper()
	t.Cleanup(func() { _ = file.Close() })
}

func writeProfileContent(t *testing.T, content []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profiles.jsonl")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func validFrontierProfile(t *testing.T) yagocrawlcontract.CrawlProfile {
	t.Helper()
	profile, err := mapYaCyProfile(yaCyProfileRecord{
		Type: "yacy-crawl-profile", Version: 1, Handle: "p", Fields: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
