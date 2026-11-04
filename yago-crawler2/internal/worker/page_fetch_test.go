package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/httpfetch"
	"github.com/D4rk4/yago/yagoegress"
)

func TestFetchRobotsPageBoundsOnlyItsNetworkRequest(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	fetcher, err := httpfetch.NewPageFetcher(httpfetch.Limits{
		RequestTimeout:      time.Second * 10,
		ConnectTimeout:      time.Second,
		TLSHandshakeTimeout: time.Second,
		HeaderTimeout:       time.Second * 10,
		MaximumRedirects:    0,
		MaximumResponseSize: 1024,
		MaximumIdleHosts:    1,
		MaximumHostActive:   1,
		UserAgent:           "yago-test",
	}, yagoegress.NewGuard(true))
	if err != nil {
		t.Fatalf("create page fetcher: %v", err)
	}
	worker := &Worker{fetcher: fetcher, hosts: newHostAdmission(0, 1)}
	startedAt := time.Now()
	_, err = worker.fetchRobotsPage(context.Background(), server.URL+"/robots.txt")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("robots request error = %v, want its bounded deadline", err)
	}
	if elapsed := time.Since(startedAt); elapsed > robotsFetchTimeout+time.Second {
		t.Fatalf("robots request elapsed %s, over its bounded network time", elapsed)
	}
	select {
	case <-started:
	default:
		t.Fatal("robots fetch did not reach the test server")
	}
}
