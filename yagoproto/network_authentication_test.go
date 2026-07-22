package yagoproto_test

import (
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestNetworkUnitDefaultsToFreeworld(t *testing.T) {
	t.Parallel()

	if got := yagoproto.NetworkUnit(""); got != yagoproto.DefaultNetwork {
		t.Fatalf("NetworkUnit(\"\") = %q, want %q", got, yagoproto.DefaultNetwork)
	}

	if got := yagoproto.NetworkUnit("intranet"); got != "intranet" {
		t.Fatalf("NetworkUnit(\"intranet\") = %q, want intranet", got)
	}
}

func TestMagicMD5IsStableHexDigest(t *testing.T) {
	t.Parallel()

	got := yagoproto.MagicMD5("key", "iam", "essentials")
	want := "153e7e6a5187ecdb2fad0620f968e6c4"

	if got != want {
		t.Fatalf("MagicMD5 = %q, want %q", got, want)
	}

	if got == yagoproto.MagicMD5("key", "iam", "other") {
		t.Fatal("MagicMD5 must depend on essentials")
	}
}

func TestNetworkAccessSignsAndAuthorizesSaltedMagic(t *testing.T) {
	access := yagoproto.NetworkAccess{
		NetworkName: "private",
		Mode:        yagoproto.NetworkAuthenticationSaltedMagic,
		Essentials:  "shared-secret",
		Self:        yagomodel.WordHash("self"),
	}
	form := url.Values{}
	access.SignWithSalt(form, "salt1234")

	if got := form.Get(yagoproto.FieldNetworkName); got != "private" {
		t.Fatalf("network = %q, want private", got)
	}
	if got := form.Get(yagoproto.FieldKey); got != "salt1234" {
		t.Fatalf("key = %q, want salt1234", got)
	}
	if !access.Authorizes(form) {
		t.Fatal("signed request was not authorized")
	}

	form.Set(yagoproto.FieldMagicMD5, "00000000000000000000000000000000")
	if access.Authorizes(form) {
		t.Fatal("wrong magic digest was authorized")
	}
}

func TestNetworkAccessSignProducesAuthorizableSalt(t *testing.T) {
	access := yagoproto.NetworkAccess{
		Mode:       yagoproto.NetworkAuthenticationSaltedMagic,
		Essentials: "shared-secret",
		Self:       yagomodel.WordHash("self"),
	}
	form := url.Values{}

	if err := access.Sign(form); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if got := form.Get(yagoproto.FieldNetworkName); got != yagoproto.DefaultNetwork {
		t.Fatalf("network = %q, want %q", got, yagoproto.DefaultNetwork)
	}
	if got := form.Get(yagoproto.FieldIam); got != access.Self.String() {
		t.Fatalf("iam = %q, want %q", got, access.Self.String())
	}
	salt := form.Get(yagoproto.FieldKey)
	decoded, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		t.Fatalf("decode salt %q: %v", salt, err)
	}
	if len(decoded) != 6 {
		t.Fatalf("decoded salt length = %d, want 6", len(decoded))
	}
	if !access.Authorizes(form) {
		t.Fatal("signed request was not authorized")
	}
}

func TestNetworkAccessSignWithSaltRemovesMagicOutsideSaltedMode(t *testing.T) {
	access := yagoproto.NetworkAccess{Self: yagomodel.WordHash("self")}
	form := url.Values{yagoproto.FieldMagicMD5: {"stale"}}

	access.SignWithSalt(form, "salt1234")

	if form.Has(yagoproto.FieldMagicMD5) {
		t.Fatalf("magic = %q, want absent", form.Get(yagoproto.FieldMagicMD5))
	}
	if !access.Authorizes(form) {
		t.Fatal("default uncontrolled request was not authorized")
	}
}

func TestNetworkAccessUncontrolledStillChecksNetwork(t *testing.T) {
	access := yagoproto.NetworkAccess{
		NetworkName: "freeworld",
		Mode:        yagoproto.NetworkAuthenticationUncontrolled,
	}

	if !access.Authorizes(url.Values{}) {
		t.Fatal("default freeworld request was rejected")
	}
	if access.Authorizes(url.Values{yagoproto.FieldNetworkName: {"private"}}) {
		t.Fatal("foreign network was authorized")
	}
}

func TestNetworkAccessDistinguishesAbsentAndEmptyNetwork(t *testing.T) {
	t.Parallel()

	for _, mode := range []yagoproto.NetworkAuthenticationMode{
		yagoproto.NetworkAuthenticationUncontrolled,
		yagoproto.NetworkAuthenticationSaltedMagic,
	} {
		t.Run(string(mode), func(t *testing.T) {
			access := yagoproto.NetworkAccess{
				Mode:       mode,
				Essentials: "shared-secret",
			}
			form := url.Values{
				yagoproto.FieldKey: {"salt1234"},
				yagoproto.FieldIam: {"opaque-caller"},
			}
			if mode == yagoproto.NetworkAuthenticationSaltedMagic {
				form.Set(
					yagoproto.FieldMagicMD5,
					yagoproto.MagicMD5("salt1234", "opaque-caller", "shared-secret"),
				)
			}

			if !access.Authorizes(form) {
				t.Fatal("absent network was not defaulted to freeworld")
			}
			form.Set(yagoproto.FieldNetworkName, "")
			if access.Authorizes(form) {
				t.Fatal("explicitly empty network was authorized as freeworld")
			}
			form.Set(yagoproto.FieldNetworkName, yagoproto.DefaultNetwork)
			if !access.Authorizes(form) {
				t.Fatal("explicit freeworld network was rejected")
			}
		})
	}
}

func TestNetworkAccessRejectsUnknownMode(t *testing.T) {
	access := yagoproto.NetworkAccess{
		NetworkName: "freeworld",
		Mode:        yagoproto.NetworkAuthenticationMode("unknown"),
	}
	if access.Authorizes(url.Values{}) {
		t.Fatal("unknown authentication mode was authorized")
	}
}
