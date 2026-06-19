package yacycrawler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func TestFrontierFollowsLinksWithinDepthAndHost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `<a href="/a">a</a><a href="/b">b</a>`+
			`<a href="http://elsewhere.invalid/x">off</a>`)
	})
	mux.HandleFunc("/a", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `<a href="/c">c</a><a href="/b">b again</a>`)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `leaf`)
	})
	mux.HandleFunc("/c", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(t, w, `<a href="/deep">deep</a>`)
	})
	mux.HandleFunc("/deep", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("requested /deep beyond max depth")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	jobs := yacycrawler.NewJobQueue(16)
	ingest := yacycrawler.NewBoundedQueue[yacycrawler.IngestBatch](16)
	fetcher := yacycrawler.NewPageFetcher(
		server.Client(),
		yacycrawler.DefaultMaxBodyBytes,
		yacycrawler.DefaultUserAgent,
	)
	publisher := yacycrawler.NewIngestPublisher(ingest)
	frontier := yacycrawler.NewFrontier(jobs, jobs.Close, 2, true)
	pipeline := yacycrawler.NewPipeline(
		jobs,
		fetcher,
		publisher,
		frontier,
		yacycrawler.NewBotWallDetector(),
	)
	node := yacycrawler.NewFakeNodeIngest(ingest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeDone := make(chan struct{})
	go func() {
		node.Run(ctx)
		close(nodeDone)
	}()
	workersDone := make(chan struct{})
	go func() {
		pipeline.RunWorkers(ctx, 3)
		close(workersDone)
	}()

	frontier.Seed(ctx, []string{server.URL})
	<-workersDone
	ingest.Close()
	<-nodeDone

	visited := map[string]bool{}
	for _, batch := range node.Batches() {
		visited[batch.SourceURL] = true
	}
	for _, want := range []string{server.URL, server.URL + "/a", server.URL + "/b", server.URL + "/c"} {
		if !visited[want] {
			t.Errorf("expected %s to be crawled, visited=%v", want, visited)
		}
	}
	if len(node.Batches()) != 4 {
		t.Errorf("expected 4 unique pages, got %d: %v", len(node.Batches()), visited)
	}
}

func writeHTML(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write([]byte("<html><body>" + body + "</body></html>")); err != nil {
		t.Errorf("write body: %v", err)
	}
}
