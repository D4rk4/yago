package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestExportCorpusResumesAfterCommittedCursorPage(t *testing.T) {
	output := filepath.Join(t.TempDir(), "corpus.jsonl")
	failedContinuation := false
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursorMark")
		cursors = append(cursors, cursor)
		if r.URL.Query().Get("q") != "*:*" || r.URL.Query().Get("sort") != "id asc" {
			t.Errorf("Solr cursor query = %v", r.URL.Query())
		}
		if cursor == "*" {
			_, _ = io.WriteString(
				w,
				`{"response":{"numFound":2,"docs":[{"id":"a","sku":"https://a.example.test/"}]},"nextCursorMark":"A"}`,
			)
			return
		}
		if !failedContinuation {
			failedContinuation = true
			http.Error(w, "temporary source failure", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(
			w,
			`{"response":{"numFound":2,"docs":[{"id":"b","sku":"https://b.example.test/"}]},"nextCursorMark":"A"}`,
		)
	}))
	defer server.Close()
	options := exportOptions{
		SolrURL:  server.URL,
		Output:   output,
		SourceID: "synthetic-fixture",
		Rows:     1,
	}
	if err := exportCorpus(context.Background(), options, io.Discard); err == nil {
		t.Fatal("transient continuation failure was ignored")
	}
	state, found, err := loadExportCheckpoint(output + ".state.json")
	if err != nil || !found || state.Cursor != "A" || state.Records != 1 {
		t.Fatalf("committed checkpoint = %+v found=%t err=%v", state, found, err)
	}
	if err := exportCorpus(context.Background(), options, io.Discard); err != nil {
		t.Fatalf("resume export: %v", err)
	}
	state, found, err = loadExportCheckpoint(output + ".state.json")
	if err != nil || !found || !state.Complete || state.Records != 2 {
		t.Fatalf("final checkpoint = %+v found=%t err=%v", state, found, err)
	}
	assertExportLineCount(t, output, 3)
	if len(cursors) != 3 || cursors[0] != "*" || cursors[1] != "A" || cursors[2] != "A" {
		t.Fatalf("cursor sequence = %#v", cursors)
	}
}

func assertExportLineCount(t *testing.T, path string, want int) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(file)
	lines := 0
	for scanner.Scan() {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatalf("invalid interchange line: %v", err)
		}
		lines++
	}
	scanErr := scanner.Err()
	closeErr := file.Close()
	if scanErr != nil {
		t.Fatalf("scan interchange file: %v", scanErr)
	}
	if closeErr != nil {
		t.Fatalf("close interchange file: %v", closeErr)
	}
	if lines != want {
		t.Fatalf("output rows = %d, want %d", lines, want)
	}
}

func TestExportDryRunWritesNoFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("rows") != "0" {
			t.Errorf("dry-run rows = %q, want zero", r.URL.Query().Get("rows"))
		}
		_, _ = io.WriteString(w, `{"response":{"numFound":42,"docs":[]},"nextCursorMark":"*"}`)
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "should-not-exist.jsonl")
	options := exportOptions{
		SolrURL:  server.URL,
		Output:   output,
		SourceID: "fixture",
		Rows:     4,
		DryRun:   true,
	}
	if err := exportCorpus(context.Background(), options, io.Discard); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("dry-run output exists: %v", err)
	}
}

func TestExportResumeRejectsDifferentSolrEndpoint(t *testing.T) {
	output := filepath.Join(t.TempDir(), "corpus.jsonl")
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursorMark") == "*" {
			_, _ = io.WriteString(w, `{"response":{"numFound":1,"docs":[]} ,"nextCursorMark":"A"}`)
			return
		}
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer first.Close()
	options := exportOptions{
		SolrURL:  first.URL,
		Output:   output,
		SourceID: "fixture-source",
		Rows:     1,
	}
	if err := exportCorpus(context.Background(), options, io.Discard); err == nil {
		t.Fatal("first export unexpectedly completed")
	}
	secondRequests := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondRequests++
		_, _ = io.WriteString(w, `{"response":{"numFound":0,"docs":[]} ,"nextCursorMark":"*"}`)
	}))
	defer second.Close()
	options.SolrURL = second.URL
	if err := exportCorpus(context.Background(), options, io.Discard); err == nil {
		t.Fatal("export resumed against a different Solr endpoint")
	}
	if secondRequests != 0 {
		t.Fatalf("different endpoint received %d requests", secondRequests)
	}
}
