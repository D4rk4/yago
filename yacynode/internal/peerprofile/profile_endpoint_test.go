package peerprofile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

type recordingProfile struct {
	consulted bool
}

func (r *recordingProfile) Properties(context.Context) []Property {
	r.consulted = true

	return []Property{
		{Key: "nickname", Value: "Yago"},
		{Key: "statement", Value: "line\r\nnext"},
	}
}

func TestProfileExportsProperties(t *testing.T) {
	profile := &recordingProfile{}
	resp, err := endpoint{
		identity: localIdentity(),
		profile:  profile,
	}.Serve(t.Context(), yacyproto.ProfileRequest{NetworkName: "freeworld"})
	if err != nil {
		t.Fatal(err)
	}

	if resp.ContentType != profileContentType {
		t.Fatalf("ContentType = %q", resp.ContentType)
	}
	if !profile.consulted {
		t.Fatal("profile source was not consulted")
	}
	if resp.Body != "nickname=Yago\r\nstatement=line\\nnext\r\n" {
		t.Fatalf("Body = %q", resp.Body)
	}
}

func TestProfileRejectsForeignNetwork(t *testing.T) {
	profile := &recordingProfile{}
	resp, err := endpoint{
		identity: localIdentity(),
		profile:  profile,
	}.Serve(t.Context(), yacyproto.ProfileRequest{NetworkName: "othernet"})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Body != "" {
		t.Fatalf("Body = %q, want empty", resp.Body)
	}
	if profile.consulted {
		t.Fatal("profile source consulted for foreign network")
	}
}

func TestNoPeerProfileReturnsNoProperties(t *testing.T) {
	profile := NoPeerProfile{}
	if got := profile.Properties(t.Context()); len(got) != 0 {
		t.Fatalf("properties = %v, want empty", got)
	}
}

func TestEncodePropertiesSkipsEmptyFieldsAndKeepsOrder(t *testing.T) {
	properties := []Property{
		{Key: "first", Value: "1"},
		{Key: "", Value: "missing"},
		{Key: "empty", Value: ""},
		{Key: "second", Value: "2"},
	}

	encoded := encodeProperties(properties)
	if encoded != "first=1\r\nsecond=2\r\n" {
		t.Fatalf("encoded = %q", encoded)
	}
	if !slices.Equal(properties, []Property{
		{Key: "first", Value: "1"},
		{Key: "", Value: "missing"},
		{Key: "empty", Value: ""},
		{Key: "second", Value: "2"},
	}) {
		t.Fatalf("properties mutated: %v", properties)
	}
}

func TestMountServesProfileRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	profile := &recordingProfile{}
	Mount(router, localIdentity(), profile)
	form := yacyproto.ProfileRequest{NetworkName: "freeworld"}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yacyproto.PathProfile+"?"+form.Encode(),
		nil,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "nickname=Yago\r\nstatement=line\\nnext\r\n" {
		t.Fatalf("Body = %q", rec.Body.String())
	}
}

func localIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{NetworkName: "freeworld"}
}
