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
		"Evaluate current peer back-ping evidence",
		"an explicitly configured public YaCy query is used only when no peer observation exists",
		`action="/admin/network/public-endpoint/test"`,
		"Check reachability",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("self-test action missing %q", want)
		}
	}
	without := do(t, New(Options{Network: fakeNetwork{snap: status}}), networkPath).body
	if strings.Contains(without, "Check reachability") {
		t.Fatal("self-test action rendered without a source")
	}
}

func TestConsoleNetworkReachabilityCheckRendersSourceAccurateResult(t *testing.T) {
	t.Parallel()

	tester := &fakeNetworkSelfTester{status: NetworkStatus{
		Available: true, RosterAvailable: true, PublicReachable: true,
		PublicReachabilityKnown: true, PublicReachabilitySource: PublicReachabilityPeerBackPing,
	}}
	console := New(Options{
		Network: fakeNetwork{snap: NetworkStatus{
			Available: true, RosterAvailable: true, PublicReachable: false,
		}},
		NetworkSelfTest: tester,
	})
	got := doPost(t, console, networkSelfTestPath, url.Values{})
	if got.status != http.StatusOK || tester.calls != 1 ||
		!strings.Contains(got.body, "current peer back-ping confirmed") ||
		!strings.Contains(got.body, `cds-metric__value">Reachable`) {
		t.Fatalf(
			"reachability check = status %d calls %d body %s",
			got.status,
			tester.calls,
			got.body,
		)
	}

	tester.status.PublicReachable = false
	unreachable := doPost(t, console, networkSelfTestPath, url.Values{})
	if !strings.Contains(unreachable.body, "peer back-ping evidence reports") {
		t.Fatalf("unreachable reachability check = %s", unreachable.body)
	}
}

func TestPublicReachabilityResultExplainsEveryEvidenceSource(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     NetworkStatus
		wantNotice string
		wantError  string
	}{
		{name: "unknown", status: NetworkStatus{}, wantError: "No current public endpoint"},
		{name: "peer reachable", status: NetworkStatus{PublicReachabilityKnown: true, PublicReachable: true, PublicReachabilitySource: PublicReachabilityPeerBackPing}, wantNotice: "peer back-ping confirmed"},
		{name: "peer unreachable", status: NetworkStatus{PublicReachabilityKnown: true, PublicReachabilitySource: PublicReachabilityPeerBackPing}, wantError: "peer back-ping evidence"},
		{name: "pinned reachable", status: NetworkStatus{PublicReachabilityKnown: true, PublicReachable: true, PublicReachabilitySource: PublicReachabilityPinnedProbe}, wantNotice: "configured direct YaCy query confirmed"},
		{name: "pinned unreachable", status: NetworkStatus{PublicReachabilityKnown: true, PublicReachabilitySource: PublicReachabilityPinnedProbe}, wantError: "configured direct YaCy query could not"},
		{name: "derived unknown", status: NetworkStatus{PublicReachabilitySource: PublicReachabilityDerivedProbe}, wantError: "local compatibility query cannot establish"},
		{name: "legacy reachable", status: NetworkStatus{PublicReachabilityKnown: true, PublicReachable: true}, wantNotice: "current reachability check confirmed"},
		{name: "legacy unreachable", status: NetworkStatus{PublicReachabilityKnown: true}, wantError: "current reachability check could not"},
	} {
		t.Run(test.name, func(t *testing.T) {
			notice, failure := publicReachabilityResult(test.status)
			if !strings.Contains(notice, test.wantNotice) ||
				!strings.Contains(failure, test.wantError) {
				t.Fatalf("result = %q/%q", notice, failure)
			}
		})
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
		!strings.Contains(failed.body, "public endpoint reachability check could not run") ||
		strings.Contains(failed.body, "private probe detail") {
		t.Fatalf("failed self-test = status %d body %s", failed.status, failed.body)
	}
	missing := doPost(t, New(Options{}), networkSelfTestPath, url.Values{})
	if missing.status != http.StatusNotFound {
		t.Fatalf("missing self-test status = %d", missing.status)
	}
}
