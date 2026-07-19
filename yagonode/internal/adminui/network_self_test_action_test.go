package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeNetworkSelfTester struct {
	status NetworkStatus
	err    error
	calls  int
}

func (f *fakeNetworkSelfTester) TestPublicEndpoint(
	context.Context,
) (NetworkStatus, error) {
	f.calls++

	return f.status, f.err
}

func TestConsoleNetworkRendersExplicitSelfTestAction(t *testing.T) {
	t.Parallel()

	status := NetworkStatus{Available: true, RosterAvailable: true}
	body := do(t, New(Options{
		Network:         fakeNetwork{snap: status},
		NetworkSelfTest: &fakeNetworkSelfTester{},
	}), networkPath).body
	for _, want := range []string{
		"Run a fresh bounded YaCy query",
		`action="/admin/network/public-endpoint/test"`,
		"Run self-test",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("self-test action missing %q", want)
		}
	}
	without := do(t, New(Options{Network: fakeNetwork{snap: status}}), networkPath).body
	if strings.Contains(without, "Run self-test") {
		t.Fatal("self-test action rendered without a source")
	}
}

func TestConsoleNetworkSelfTestRendersFreshResult(t *testing.T) {
	t.Parallel()

	tester := &fakeNetworkSelfTester{status: NetworkStatus{
		Available: true, RosterAvailable: true, PublicReachable: true,
	}}
	console := New(Options{
		Network: fakeNetwork{snap: NetworkStatus{
			Available: true, RosterAvailable: true, PublicReachable: false,
		}},
		NetworkSelfTest: tester,
	})
	got := doPost(t, console, networkSelfTestPath, url.Values{})
	if got.status != http.StatusOK || tester.calls != 1 ||
		!strings.Contains(got.body, "fresh self-test confirmed") ||
		!strings.Contains(got.body, `cds-metric__value">Reachable`) {
		t.Fatalf("fresh self-test = status %d calls %d body %s", got.status, tester.calls, got.body)
	}

	tester.status.PublicReachable = false
	unreachable := doPost(t, console, networkSelfTestPath, url.Values{})
	if !strings.Contains(unreachable.body, "fresh self-test could not reach") {
		t.Fatalf("unreachable self-test = %s", unreachable.body)
	}
}

func TestConsoleNetworkSelfTestFailureIsGeneric(t *testing.T) {
	t.Parallel()

	tester := &fakeNetworkSelfTester{err: errors.New("private probe detail")}
	failed := doPost(t, New(Options{
		Network:         fakeNetwork{snap: NetworkStatus{Available: true, RosterAvailable: true}},
		NetworkSelfTest: tester,
	}), networkSelfTestPath, url.Values{})
	if failed.status != http.StatusOK ||
		!strings.Contains(failed.body, "public endpoint self-test could not run") ||
		strings.Contains(failed.body, "private probe detail") {
		t.Fatalf("failed self-test = status %d body %s", failed.status, failed.body)
	}
	missing := doPost(t, New(Options{}), networkSelfTestPath, url.Values{})
	if missing.status != http.StatusNotFound {
		t.Fatalf("missing self-test status = %d", missing.status)
	}
}
