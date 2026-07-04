package peerprofile

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
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
	}.Serve(t.Context(), yagoproto.ProfileRequest{NetworkName: "freeworld"})
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
	}.Serve(t.Context(), yagoproto.ProfileRequest{NetworkName: "othernet"})
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

func TestProfileFileReturnsNoPropertiesWhenMissingOrCanceled(t *testing.T) {
	profile := ProfileFile{files: fstest.MapFS{}}
	if got := profile.Properties(t.Context()); len(got) != 0 {
		t.Fatalf("missing properties = %v, want empty", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := profile.Properties(ctx); len(got) != 0 {
		t.Fatalf("canceled properties = %v, want empty", got)
	}
}

func TestProfileFileReadsProperties(t *testing.T) {
	profile := ProfileFile{files: fstest.MapFS{
		profileFileName: {Data: []byte("" +
			"# ignored\n" +
			"nickname=Yago\n" +
			"statement: line\\nnext\n" +
			"city Podgorica\n" +
			"unicode=Go\\u0020Peer\n" +
			"spaced = value\n" +
			"escaped\\=key=escaped\\:value\n" +
			"empty=\n" +
			"bad\n")},
	}}

	got := profile.Properties(t.Context())
	want := []Property{
		{Key: "nickname", Value: "Yago"},
		{Key: "statement", Value: "line\nnext"},
		{Key: "city", Value: "Podgorica"},
		{Key: "unicode", Value: "Go Peer"},
		{Key: "spaced", Value: "value"},
		{Key: "escaped=key", Value: "escaped:value"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("properties = %v, want %v", got, want)
	}
}

func TestNewProfileFileReadsDataDirectoryProfile(t *testing.T) {
	dataDir := t.TempDir()
	settingsDir := filepath.Join(dataDir, "SETTINGS")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(settingsDir, "profile.txt"),
		[]byte("operator=alice\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	got := NewProfileFile(dataDir).Properties(t.Context())
	want := []Property{{Key: "operator", Value: "alice"}}
	if !slices.Equal(got, want) {
		t.Fatalf("properties = %v, want %v", got, want)
	}
}

func TestProfileFileReturnsNoPropertiesOnReadError(t *testing.T) {
	profile := ProfileFile{files: fstest.MapFS{profileFileName: {Mode: fs.ModeDir}}}
	if got := profile.Properties(t.Context()); len(got) != 0 {
		t.Fatalf("properties = %v, want empty on read error", got)
	}
}

func TestProfilePropertyUnescapeKeepsMalformedUnicodeEscape(t *testing.T) {
	got := unescapeProfileProperty(`bad\u00zz`)
	if got != `bad\u00zz` {
		t.Fatalf("unescaped = %q, want malformed escape unchanged", got)
	}

	got = unescapeProfileProperty(`bad\u0`)
	if got != `bad\u0` {
		t.Fatalf("short unicode unescaped = %q, want malformed escape unchanged", got)
	}
}

func TestProfilePropertyUnescapeSupportsJavaEscapes(t *testing.T) {
	got := unescapeProfileProperty(`a\rb\tc\fd\x`)
	if got != "a\rb\tc\fdx" {
		t.Fatalf("unescaped = %q", got)
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
	form := yagoproto.ProfileRequest{NetworkName: "freeworld"}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yagoproto.PathProfile+"?"+form.Encode(),
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

func TestMountUsesEmptyProfileWhenNil(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	Mount(router, localIdentity(), nil)
	form := yagoproto.ProfileRequest{NetworkName: "freeworld"}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yagoproto.PathProfile+"?"+form.Encode(),
		nil,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "" {
		t.Fatalf("Body = %q, want empty", rec.Body.String())
	}
}

func localIdentity() nodeidentity.Identity {
	return nodeidentity.Identity{NetworkName: "freeworld"}
}
