package yacysearch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

type progressReportingSearcher struct {
	response searchcore.Response
	err      error
	calls    int
}

type plainProgressiveWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (writer *plainProgressiveWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = make(http.Header)
	}

	return writer.header
}

func (writer *plainProgressiveWriter) WriteHeader(status int) {
	writer.status = status
}

func (writer *plainProgressiveWriter) Write(data []byte) (int, error) {
	_, _ = writer.body.Write(data)
	return len(data), nil
}

type failingProgressiveWriter struct {
	plainProgressiveWriter
	writes int
	failAt int
}

func (writer *failingProgressiveWriter) Write(data []byte) (int, error) {
	writer.writes++
	if writer.writes == writer.failAt {
		return 0, errors.New("client disconnected")
	}

	_, _ = writer.body.Write(data)
	return len(data), nil
}

func (*failingProgressiveWriter) Flush() {}

func (searcher *progressReportingSearcher) Search(
	ctx context.Context,
	request searchcore.Request,
) (searchcore.Response, error) {
	searcher.calls++
	searchcore.ReportSearchProgress(ctx, searchcore.SearchProgress{
		Response: searchcore.Response{
			Request: request,
			Results: []searchcore.Result{
				{
					Title:   "Partial title",
					URL:     "https://partial.example/page",
					Snippet: "partial text",
				},
			},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 2, SampledShards: 1, TotalShards: 8, Complete: false,
			},
			PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "incomplete shard coverage",
			}},
		},
		CompletedShards: 2, SampledShards: 1, TotalShards: 8,
	})
	searcher.response.Request = request

	return searcher.response, searcher.err
}

func TestProgressiveJSONEndpointStreamsPartialAndFinalResponses(t *testing.T) {
	searcher := &progressReportingSearcher{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title: "Final title", URL: "https://final.example/page", Snippet: "final text",
		}},
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceLocalSearch,
			Reason: "deadline",
		}},
	}}
	mux := http.NewServeMux()
	Mount(mux, searcher, searcher, false, ClickCapture{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathYaCySearchNDJSON+"?query=topic",
		nil,
	)
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK ||
		recorder.Header().Get("Content-Type") != "application/x-ndjson; charset=utf-8" {
		t.Fatalf(
			"status=%d headers=%v body=%s",
			recorder.Code,
			recorder.Header(),
			recorder.Body.String(),
		)
	}
	var events []progressiveJSONEvent
	scanner := bufio.NewScanner(recorder.Body)
	for scanner.Scan() {
		var event progressiveJSONEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Type != "started" ||
		events[1].Type != "progress" || events[2].Type != "final" {
		t.Fatalf("stream events = %+v", events)
	}
	if events[1].Complete || events[1].CompletedShards != 2 ||
		events[1].TotalShards != 8 || events[1].Response == nil ||
		events[1].Response.Coverage == nil ||
		events[1].Response.Coverage.SampledShards != 1 ||
		events[1].Response.Channels[0].Items[0].Title != "Partial title" {
		t.Fatalf("partial event = %+v", events[1])
	}
	if events[2].Complete || events[2].Response == nil ||
		events[2].Response.Channels[0].Items[0].Title != "Final title" || searcher.calls != 1 {
		t.Fatalf("final event=%+v calls=%d", events[2], searcher.calls)
	}
}

func TestProgressiveJSONEndpointRejectsWrongMethodAndMalformedQuery(t *testing.T) {
	endpoint := progressiveJSONEndpoint{search: &progressReportingSearcher{}}
	for _, test := range []struct {
		method string
		target string
		status int
	}{
		{method: http.MethodPost, target: yagoproto.PathYaCySearchNDJSON, status: http.StatusMethodNotAllowed},
		{method: http.MethodGet, target: yagoproto.PathYaCySearchNDJSON + "?count=invalid", status: http.StatusBadRequest},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), test.method, test.target, nil)
		endpoint.ServeHTTP(recorder, request)
		if recorder.Code != test.status {
			t.Fatalf(
				"%s %s status=%d, want %d",
				test.method,
				test.target,
				recorder.Code,
				test.status,
			)
		}
	}
}

func TestProgressiveHTMLShellDoesNotRunSearchUntilStreamStarts(t *testing.T) {
	searcher := &progressReportingSearcher{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/yacysearch.html?query=topic&progressive=1",
		nil,
	)
	htmlEndpoint{search: searcher, suggestions: newRecentQueries()}.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || searcher.calls != 0 ||
		!strings.Contains(body, "Searching the local index") ||
		!strings.Contains(body, "/yacysearch.ndjson?") ||
		strings.Contains(body, `id="nothing-found"`) {
		t.Fatalf(
			"status=%d calls=%d unexpected progressive shell: %s",
			recorder.Code,
			searcher.calls,
			body,
		)
	}
}

func TestProgressiveJSONEndpointHandlesUnavailableWriterAndSearchFailure(t *testing.T) {
	searcher := &progressReportingSearcher{}
	endpoint := progressiveJSONEndpoint{search: searcher, suggestions: newRecentQueries()}
	request := func() *http.Request {
		return httptest.NewRequestWithContext(
			t.Context(), http.MethodGet, yagoproto.PathYaCySearchNDJSON+"?query=topic", nil,
		)
	}

	plain := &plainProgressiveWriter{}
	endpoint.ServeHTTP(plain, request())
	if plain.status != http.StatusInternalServerError || searcher.calls != 0 {
		t.Fatalf("plain writer status=%d search calls=%d", plain.status, searcher.calls)
	}

	failedStart := &failingProgressiveWriter{failAt: 1}
	endpoint.ServeHTTP(failedStart, request())
	if searcher.calls != 0 {
		t.Fatalf("search ran after the started event failed: %d calls", searcher.calls)
	}

	searcher.err = errors.New("search failed")
	recorder := httptest.NewRecorder()
	endpoint.ServeHTTP(recorder, request())
	if recorder.Code != http.StatusOK ||
		!strings.Contains(recorder.Body.String(), `"type":"error"`) {
		t.Fatalf("error stream status=%d body=%q", recorder.Code, recorder.Body.String())
	}

	searcher.err = nil
	failedProgress := &failingProgressiveWriter{failAt: 2}
	endpoint.ServeHTTP(failedProgress, request())
	if searcher.calls != 2 {
		t.Fatalf("search calls after failed progress write=%d", searcher.calls)
	}
	if strings.Contains(failedProgress.body.String(), `"type":"progress"`) {
		t.Fatal("failed progress event unexpectedly appeared in the stream")
	}

	success := httptest.NewRecorder()
	endpoint.ServeHTTP(success, request())
	if !strings.Contains(success.Body.String(), `"type":"final"`) ||
		endpoint.suggestions == nil {
		t.Fatalf("successful progressive stream=%q", success.Body.String())
	}
}
