package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestImportFrontierIsResumableAndDeduplicatesURLProfilePairs(t *testing.T) {
	input, profiles := writeFrontierFixtures(t, []yaCyFrontierRecord{
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://example.test/one",
			ProfileHandle: "source-profile",
			Depth:         1,
		},
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://example.test/two",
			ProfileHandle: "source-profile",
			Depth:         2,
		},
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://example.test/one",
			ProfileHandle: "source-profile",
			Depth:         1,
		},
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://example.test/three",
			ProfileHandle: "source-profile",
			Depth:         3,
		},
	})
	var received []yagocrawlcontract.CrawlOrderImportBatch
	server := newFrontierImportRecordingServer(t, &received)
	defer server.Close()

	state := filepath.Join(t.TempDir(), "frontier-state.json")
	opts := frontierImportOptions{
		Input: input, Profiles: profiles, Endpoint: server.URL + "/admin/crawl/frontier/import",
		APIKey: "test-key", State: state, BatchRecords: 2, MaxRecords: 2,
	}
	if err := importFrontier(context.Background(), opts, &strings.Builder{}); err != nil {
		t.Fatalf("first frontier pass: %v", err)
	}
	checkpoint, found, err := loadFrontierImportCheckpoint(state)
	if err != nil || !found || checkpoint.Records != 2 || checkpoint.EnqueuedRequests != 2 ||
		checkpoint.Complete {
		t.Fatalf("first checkpoint = %+v, found=%t, err=%v", checkpoint, found, err)
	}

	opts.MaxRecords = 0
	if err := importFrontier(context.Background(), opts, &strings.Builder{}); err != nil {
		t.Fatalf("resumed frontier pass: %v", err)
	}
	checkpoint, _, err = loadFrontierImportCheckpoint(state)
	if err != nil {
		t.Fatal(err)
	}
	if !checkpoint.Complete || checkpoint.Records != 4 || checkpoint.UniqueRequests != 3 ||
		checkpoint.EnqueuedRequests != 3 || checkpoint.DuplicateRequests != 1 {
		t.Fatalf("completed checkpoint = %+v", checkpoint)
	}
	if len(received) != 2 || len(received[0].Requests) != 2 || len(received[1].Requests) != 1 {
		t.Fatalf("posted batch sizes = %v", frontierBatchSizes(received))
	}
	if got := received[1].Requests[0].URL; got != "https://example.test/three" {
		t.Fatalf("resume requeued a duplicate instead of the remaining URL: %q", got)
	}
	if err := importFrontier(context.Background(), opts, &strings.Builder{}); err != nil {
		t.Fatalf("completed frontier rerun: %v", err)
	}
	if len(received) != 2 {
		t.Fatalf("completed rerun submitted %d additional batches", len(received)-2)
	}
}

func newFrontierImportRecordingServer(
	t *testing.T,
	received *[]yagocrawlcontract.CrawlOrderImportBatch,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/crawl/frontier/import" || r.Method != http.MethodPost {
			t.Errorf("endpoint = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("request did not carry the admin API key")
		}
		var batch yagocrawlcontract.CrawlOrderImportBatch
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Errorf("decode batch: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := batch.Validate(); err != nil {
			t.Errorf("invalid posted batch: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		*received = append(*received, batch)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(frontierImportResponse{EnqueuedRequests: len(batch.Requests)})
	}))
}

func TestImportFrontierDryRunDoesNotCallDestinationOrWriteCheckpoint(t *testing.T) {
	credentialedURL := "https://" + url.User("fixture-user").String() + "@example.test/private"
	input, profiles := writeFrontierFixtures(t, []yaCyFrontierRecord{
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://example.test/one",
			ProfileHandle: "source-profile",
		},
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           credentialedURL,
			ProfileHandle: "source-profile",
		},
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://exa mple.test/private",
			ProfileHandle: "source-profile",
		},
	})
	checkpoint := filepath.Join(t.TempDir(), "dry-run.state")
	var output strings.Builder
	err := importFrontier(context.Background(), frontierImportOptions{
		Input: input, Profiles: profiles, State: checkpoint, BatchRecords: 4, DryRun: true,
	}, &output)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(output.String(), "blocked-credential-urls=1") ||
		!strings.Contains(output.String(), "blocked-invalid-urls=1") ||
		strings.Contains(
			output.String(),
			"fixture-user",
		) || strings.Contains(output.String(), credentialedURL) {
		t.Fatalf("dry-run output leaked or omitted credential URL disposition: %s", output.String())
	}
	if _, err := os.Stat(checkpoint); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote a checkpoint: %v", err)
	}
}

func TestImportFrontierBlocksUnsafeURLsWithoutForwardingThem(t *testing.T) {
	credentialedURL := "https://" + url.User("fixture-user").String() + "@example.test/private"
	input, profiles := writeFrontierFixtures(t, []yaCyFrontierRecord{
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           credentialedURL,
			ProfileHandle: "source-profile",
		},
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://exa mple.test/private",
			ProfileHandle: "source-profile",
		},
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://example.test/public",
			ProfileHandle: "source-profile",
		},
	})
	var received []yagocrawlcontract.CrawlOrderImportBatch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch yagocrawlcontract.CrawlOrderImportBatch
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Errorf("decode imported frontier: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received = append(received, batch)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(frontierImportResponse{EnqueuedRequests: len(batch.Requests)})
	}))
	defer server.Close()
	statePath := filepath.Join(t.TempDir(), "frontier-state.json")
	var output strings.Builder
	err := importFrontier(context.Background(), frontierImportOptions{
		Input: input, Profiles: profiles, Endpoint: server.URL + "/admin/crawl/frontier/import",
		APIKey: "test-key", State: statePath, BatchRecords: 1,
	}, &output)
	if err != nil {
		t.Fatalf("import frontier: %v", err)
	}
	if len(received) != 1 || len(received[0].Requests) != 1 ||
		received[0].Requests[0].URL != "https://example.test/public" {
		t.Fatalf("forwarded frontier batches = %+v", received)
	}
	checkpoint, found, err := loadFrontierImportCheckpoint(statePath)
	if err != nil || !found || checkpoint.BlockedCredentialURLs != 1 ||
		checkpoint.BlockedInvalidURLs != 1 ||
		checkpoint.Records != 3 ||
		checkpoint.EnqueuedRequests != 1 ||
		!checkpoint.Complete {
		t.Fatalf("frontier checkpoint = %+v, found=%t, err=%v", checkpoint, found, err)
	}
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateBytes), "fixture-user") ||
		strings.Contains(output.String(), "fixture-user") {
		t.Fatal("credentialed URL leaked into checkpoint or progress output")
	}
}

func TestImportFrontierRejectsInputMutationAndMissingProfile(t *testing.T) {
	input, profiles := writeFrontierFixtures(t, []yaCyFrontierRecord{
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://example.test/one",
			ProfileHandle: "missing-profile",
		},
	})
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	opts := frontierImportOptions{
		Input: input, Profiles: profiles, Endpoint: server.URL + "/admin/crawl/frontier/import",
		APIKey: "test-key", BatchRecords: 1,
	}
	if err := importFrontier(context.Background(), opts, &strings.Builder{}); err == nil ||
		!strings.Contains(err.Error(), "unavailable profile") {
		t.Fatalf("missing profile error = %v", err)
	}

	validInput, validProfiles := writeFrontierFixtures(t, []yaCyFrontierRecord{
		{
			Type:          "crawlRequest",
			Version:       1,
			Stack:         "LOCAL",
			URL:           "https://example.test/one",
			ProfileHandle: "source-profile",
		},
	})
	state := filepath.Join(t.TempDir(), "frontier-state.json")
	opts.Input, opts.Profiles, opts.State = validInput, validProfiles, state
	if err := importFrontier(context.Background(), opts, &strings.Builder{}); err == nil {
		t.Fatal("unexpected destination response was accepted")
	}
	if err := os.WriteFile(validInput, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := importFrontier(context.Background(), opts, &strings.Builder{}); err == nil ||
		!strings.Contains(err.Error(), "checkpoint does not match") {
		t.Fatalf("mutated input resume error = %v", err)
	}
}

func TestMapYaCyProfileAndFrontierRequestPreserveSourceFields(t *testing.T) {
	record := yaCyProfileRecord{
		Type: "yacy-crawl-profile", Version: 1, State: "active", Handle: "source-profile",
		Fields: map[string]string{
			"generalDepth": "0", "domMaxPages": "-1", "crawlerURLMustMatch": ".*",
			"indexURLMustMatch": ".*", "obeyHtmlRobotsNofollow": "true",
			"recrawlIfOlder": "0", "collections": "fixture-collection",
		},
	}
	profile, err := mapYaCyProfile(record)
	if err != nil {
		t.Fatalf("map profile: %v", err)
	}
	if profile.MaxDepth != yagocrawlcontract.MaxCrawlDepth ||
		profile.MaxPagesPerHost != yagocrawlcontract.UnlimitedPagesPerHost ||
		profile.MaxPagesPerRun == nil || *profile.MaxPagesPerRun != yagocrawlcontract.MaximumCrawlOrderImportBatchRecords ||
		profile.FollowNoFollowLinks {
		t.Fatalf("mapped profile = %+v", profile)
	}
	if profile.SourceMetadata.Get("yacy.profile.collections") != "fixture-collection" {
		t.Fatal("source collection assignment was not retained in profile metadata")
	}
	request, err := mapYaCyFrontierRequest(yaCyFrontierRecord{
		Stack: "LOCAL", URL: "https://example.test/pending", ProfileHandle: record.Handle,
		Depth: 4, Anchor: "example anchor", InitiatorHex: "",
		QueuedAtMillis: 1_700_000_000_000,
	}, profile)
	if err != nil {
		t.Fatalf("map frontier request: %v", err)
	}
	if request.Depth != 4 || request.AnchorName != "example anchor" || request.AppDate.IsZero() ||
		request.SourceMetadata.Get("yacy.stack") != "LOCAL" ||
		request.SourceMetadata.Get("yacy.source_profile_handle") != record.Handle {
		t.Fatalf("mapped request = %+v", request)
	}
}

func TestMapYaCyRecrawlCutoffRemainsSourceMetadata(t *testing.T) {
	for _, cutoff := range []string{"0", "1789809015447", "9223372036854775807", "eternity"} {
		t.Run(cutoff, func(t *testing.T) {
			record := yaCyProfileRecord{
				Type: "yacy-crawl-profile", Version: 1, Handle: "source-profile",
				Fields: map[string]string{"recrawlIfOlder": cutoff},
			}
			profile, err := mapYaCyProfile(record)
			if err != nil {
				t.Fatalf("map profile: %v", err)
			}
			if profile.RecrawlIfOlder != 0 {
				t.Fatalf(
					"RecrawlIfOlder = %v; YaCy cutoff must not become a Yago interval",
					profile.RecrawlIfOlder,
				)
			}
			if got := profile.SourceMetadata.Get("yacy.profile.recrawlIfOlder"); got != cutoff {
				t.Fatalf("source recrawl cutoff metadata = %q, want %q", got, cutoff)
			}
		})
	}
}

func TestMapYaCyProfileRejectsInvalidRecrawlCutoff(t *testing.T) {
	record := yaCyProfileRecord{
		Type: "yacy-crawl-profile", Version: 1, Handle: "source-profile",
		Fields: map[string]string{"recrawlIfOlder": "not-a-date"},
	}
	if _, err := mapYaCyProfile(record); err == nil {
		t.Fatal("invalid recrawl cutoff was accepted")
	}
}

func TestReadFrontierHeaderAcceptsYaCyExporterType(t *testing.T) {
	for _, kind := range []string{"yacy-crawl-frontier", "yacy-queue-frontier"} {
		t.Run(kind, func(t *testing.T) {
			line, err := json.Marshal(frontierFileHeader{
				Type: kind, Version: 1, SourceID: "fixture-source",
			})
			if err != nil {
				t.Fatal(err)
			}
			got, _, err := readFrontierHeader(
				bufio.NewReader(strings.NewReader(string(line) + "\n")),
			)
			if err != nil || got.Type != kind {
				t.Fatalf("read frontier header = %+v, err=%v", got, err)
			}
		})
	}
}

func writeFrontierFixtures(t *testing.T, records []yaCyFrontierRecord) (string, string) {
	t.Helper()
	directory := t.TempDir()
	input := filepath.Join(directory, "frontier.jsonl")
	profilePath := filepath.Join(directory, "profiles.jsonl")
	frontierFile, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(frontierFile)
	if err := encoder.Encode(
		frontierFileHeader{Type: "yacy-queue-frontier", Version: 1, SourceID: "fixture-source"},
	); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := frontierFile.Close(); err != nil {
		t.Fatal(err)
	}
	profileFile, err := os.Create(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	profileEncoder := json.NewEncoder(profileFile)
	if err := profileEncoder.Encode(
		frontierFileHeader{Type: "yacy-crawl-profiles", Version: 1, SourceID: "fixture-source"},
	); err != nil {
		t.Fatal(err)
	}
	if err := profileEncoder.Encode(yaCyProfileRecord{
		Type: "yacy-crawl-profile", Version: 1, State: "active", Handle: "source-profile",
		Fields: map[string]string{
			"generalDepth": "0", "domMaxPages": "-1", "crawlerURLMustMatch": ".*",
			"indexURLMustMatch": ".*", "obeyHtmlRobotsNofollow": "true", "recrawlIfOlder": "0",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := profileFile.Close(); err != nil {
		t.Fatal(err)
	}
	return input, profilePath
}

func frontierBatchSizes(batches []yagocrawlcontract.CrawlOrderImportBatch) []string {
	sizes := make([]string, len(batches))
	for index, batch := range batches {
		sizes[index] = fmt.Sprint(len(batch.Requests))
	}
	return sizes
}
