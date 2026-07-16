package adminui

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestConsoleNetworkRosterReadErrorIsUnavailable(t *testing.T) {
	t.Parallel()

	body := mainRegion(t, do(t, New(Options{Network: fakeNetwork{snap: NetworkStatus{
		Available: true,
	}}}), "/admin/network").body)
	if strings.Count(body, ">Unavailable<") != 2 ||
		!strings.Contains(body, "Peer roster is unavailable.") {
		t.Fatalf("failed roster read did not render unavailable facts: %s", body)
	}
	if strings.Contains(body, "No peers yet.") ||
		strings.Contains(body, `class="cds-metric__value">0<`) {
		t.Fatalf("failed roster read rendered an honest empty roster: %s", body)
	}
}

func TestConsolePeerDetailReadErrorIsUnavailable(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{PeerDetail: &fakePeerDetail{
		err: errors.New("roster closed"),
	}}), "/admin/network/peer?hash=HHHHHHHHHHHH")
	if got.status != http.StatusOK || !strings.Contains(got.body, peerDetailUnavailable) {
		t.Fatalf("failed peer lookup = %d %s", got.status, got.body)
	}
	if strings.Contains(got.body, "404 page not found") {
		t.Fatalf("failed peer lookup rendered as missing peer: %s", got.body)
	}
}

func TestConsoleSeedlistDistinguishesUnavailableFromNever(t *testing.T) {
	t.Parallel()

	unavailable := NetworkStatus{
		Available: true, RosterAvailable: true,
		Seedlists: []SeedlistEntry{{URL: "https://unavailable.example/seed.txt"}},
	}
	body := mainRegion(
		t,
		do(t, New(Options{Network: fakeNetwork{snap: unavailable}}), "/admin/network").body,
	)
	if !strings.Contains(body, "Unavailable") || strings.Contains(body, "never") {
		t.Fatalf("unavailable seedlist status rendered as import history: %s", body)
	}

	never := NetworkStatus{
		Available: true, RosterAvailable: true,
		Seedlists: []SeedlistEntry{{
			URL: "https://never.example/seed.txt", StatusKnown: true,
		}},
	}
	body = mainRegion(
		t,
		do(t, New(Options{Network: fakeNetwork{snap: never}}), "/admin/network").body,
	)
	if !strings.Contains(body, "never") || strings.Contains(body, ">Unavailable<") {
		t.Fatalf("known empty seedlist history rendered as unavailable: %s", body)
	}
}

func TestConsoleNetworkRendersUnknownPeerBlockStatus(t *testing.T) {
	t.Parallel()

	status := NetworkStatus{
		Available: true, RosterAvailable: true,
		Peers: []NetworkPeer{{Name: "peerA", Hash: "HHHHHHHHHHHH"}},
	}
	body := do(t, New(Options{Network: fakeNetwork{snap: status}}), "/admin/network").body
	if !strings.Contains(body, "block status unavailable") {
		t.Fatal("unknown peer block status should be visible in the peer table")
	}
}

func TestConsolePeerDetailSuppressesControlsWhenBlockStatusUnknown(t *testing.T) {
	t.Parallel()

	detail := &fakePeerDetail{ok: true, detail: PeerDetail{
		Hash: "HHHHHHHHHHHH", Blocked: true,
	}}
	body := do(t, New(Options{
		PeerDetail: detail,
		PeerBlock:  &fakePeerBlock{},
	}), "/admin/network/peer?hash=HHHHHHHHHHHH").body
	if !strings.Contains(body, "Block status is unavailable.") {
		t.Fatal("unknown block status should render unavailable")
	}
	if strings.Contains(body, `value="block"`) || strings.Contains(body, `value="unblock"`) {
		t.Fatal("unknown block status should suppress block mutations")
	}
	if strings.Contains(body, ">blocked</span>") {
		t.Fatal("unknown block status should suppress a stale blocked marker")
	}
}

func TestConsoleNetworkPeerNewsReadErrorIsUnavailable(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Network: fakeNetwork{snap: NetworkStatus{
			Available: true, RosterAvailable: true,
		}},
		PeerNews: fakePeerNews{unavailable: true},
	})
	body := do(t, console, "/admin/network").body
	if !strings.Contains(body, "Peer news is unavailable.") {
		t.Fatal("failed peer-news read should render unavailable")
	}
	if strings.Contains(body, "No peer news received yet.") {
		t.Fatal("failed peer-news read rendered an honest-empty state")
	}
}
