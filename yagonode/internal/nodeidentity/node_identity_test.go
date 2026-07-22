package nodeidentity

import (
	"net/url"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestIdentityUptimeRoundsDownToMinutes(t *testing.T) {
	start := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	identity := Identity{Start: start}

	if got := identity.Uptime(start.Add(90*time.Second + 59*time.Millisecond)); got != 1 {
		t.Fatalf("Uptime = %d, want 1", got)
	}
}

func TestIdentityUptimeSecondsKeepsSecondGranularity(t *testing.T) {
	start := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)
	identity := Identity{Start: start}

	if got := identity.UptimeSeconds(start.Add(90*time.Second + 500*time.Millisecond)); got != 90 {
		t.Fatalf("UptimeSeconds = %d, want 90", got)
	}
}

func TestIdentityNetworkMatchesDefaultNetwork(t *testing.T) {
	identity := Identity{NetworkName: "freeworld"}

	if !identity.NetworkMatches("") {
		t.Fatal("empty network should match default freeworld network")
	}
	if identity.NetworkMatches("other") {
		t.Fatal("different network should not match")
	}
}

func TestIdentityAddressesNetworkAndHash(t *testing.T) {
	hash := yagomodel.WordHash("self")
	identity := Identity{Hash: hash, NetworkName: "freeworld"}

	if !identity.Addresses("freeworld", hash) {
		t.Fatal("identity should address matching network and hash")
	}
	if identity.Addresses("freeworld", yagomodel.WordHash("other")) {
		t.Fatal("identity should reject a different hash")
	}
	if identity.Addresses("other", hash) {
		t.Fatal("identity should reject a different network")
	}
}

func TestIdentityAuthenticatesConfiguredNetwork(t *testing.T) {
	identity := Identity{
		Hash:                     yagomodel.WordHash("self"),
		NetworkName:              "private",
		AuthenticationMode:       yagoproto.NetworkAuthenticationSaltedMagic,
		AuthenticationEssentials: "shared-secret",
	}
	form := url.Values{}
	identity.NetworkAccess().SignWithSalt(form, "salt1234")

	if !identity.Authenticates(
		form.Get(yagoproto.FieldNetworkName),
		form.Has(yagoproto.FieldNetworkName),
		form.Get(yagoproto.FieldKey),
		form.Get(yagoproto.FieldIam),
		form.Get(yagoproto.FieldMagicMD5),
	) {
		t.Fatal("signed network request was not authenticated")
	}
	if identity.Authenticates("other", true, "", "", "") {
		t.Fatal("foreign network request was authenticated")
	}
}

func TestIdentityAuthenticatesMissingNetworkAndAddressesTarget(t *testing.T) {
	self := yagomodel.WordHash("self")
	identity := Identity{Hash: self, NetworkName: yagoproto.DefaultNetwork}
	if !identity.Authenticates("", false, "", "", "") || !identity.Addresses("", self) {
		t.Fatal("matching uncontrolled request was rejected")
	}
	if identity.Addresses("", yagomodel.WordHash("other")) {
		t.Fatal("request addressed to another node was authenticated")
	}
}
