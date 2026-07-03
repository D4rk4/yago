package robots_test

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yacycrawler/internal/robots"
)

type countingDenialObserver struct{ denied int }

func (c *countingDenialObserver) RobotsDenied() { c.denied++ }

func TestRobotsAdmissionRecordsDenial(t *testing.T) {
	server := robotsServer(t, "User-agent: *\nDisallow: /private\n", nil)
	defer server.Close()
	observer := &countingDenialObserver{}
	fetcher, err := robots.NewRobotsAdmissionFetcher(
		deliveringSource(), server.Client(), testUserAgent, 8,
		robots.WithDenialObserver(observer),
	)
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, server.URL+"/private/secret"),
	); err == nil {
		t.Fatal("expected robots rejection")
	}
	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, server.URL+"/public"),
	); err != nil {
		t.Fatalf("allow public: %v", err)
	}
	if observer.denied != 1 {
		t.Fatalf("denied = %d, want 1", observer.denied)
	}
}

func TestRobotsAdmissionNilDenialObserverStaysNoop(t *testing.T) {
	server := robotsServer(t, "User-agent: *\nDisallow: /private\n", nil)
	defer server.Close()
	fetcher, err := robots.NewRobotsAdmissionFetcher(
		deliveringSource(), server.Client(), testUserAgent, 8,
		robots.WithDenialObserver(nil),
	)
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, server.URL+"/private/x"),
	); err == nil {
		t.Fatal("expected robots rejection")
	}
}
