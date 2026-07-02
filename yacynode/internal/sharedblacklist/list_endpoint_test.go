package sharedblacklist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

type recordingBlacklists struct {
	name string
}

func (r *recordingBlacklists) Entries(_ context.Context, name string) []string {
	r.name = name

	return []string{"example.org/.*", "blocked.example/.*"}
}

func TestListExportsSharedBlacklistEntries(t *testing.T) {
	blacklists := &recordingBlacklists{}
	resp, err := endpoint{blacklists: blacklists}.Serve(
		t.Context(),
		yacyproto.ListRequest{Column: yacyproto.ListColumnBlack, Name: "url.default.black"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if resp.ContentType != listContentType {
		t.Fatalf("ContentType = %q", resp.ContentType)
	}
	if blacklists.name != "url.default.black" {
		t.Fatalf("name = %q", blacklists.name)
	}
	if resp.Body != "example.org/.*\r\nblocked.example/.*\r\n" {
		t.Fatalf("Body = %q", resp.Body)
	}
}

func TestListIgnoresUnsupportedColumn(t *testing.T) {
	blacklists := &recordingBlacklists{}
	resp, err := endpoint{blacklists: blacklists}.Serve(
		t.Context(),
		yacyproto.ListRequest{Column: "blue", Name: "url.default.black"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Body != "" {
		t.Fatalf("Body = %q, want empty", resp.Body)
	}
	if blacklists.name != "" {
		t.Fatalf("blacklists consulted for unsupported column: %q", blacklists.name)
	}
}

func TestNoSharedBlacklistsReturnsNoEntries(t *testing.T) {
	blacklists := NoSharedBlacklists{}
	if got := blacklists.Entries(t.Context(), "url.default.black"); len(got) != 0 {
		t.Fatalf("entries = %v, want empty", got)
	}
}

func TestEncodeEntriesKeepsOrder(t *testing.T) {
	entries := []string{"a", "b"}
	encoded := encodeEntries(entries)
	if encoded != "a\r\nb\r\n" {
		t.Fatalf("encoded = %q", encoded)
	}
	if !slices.Equal(entries, []string{"a", "b"}) {
		t.Fatalf("entries mutated: %v", entries)
	}
}

func TestEncodeEntriesEmpty(t *testing.T) {
	if got := encodeEntries(nil); got != "" {
		t.Fatalf("encoded = %q, want empty", got)
	}
}

func TestMountServesSharedBlacklistRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	blacklists := &recordingBlacklists{}
	Mount(router, blacklists)
	form := yacyproto.ListRequest{
		Column: yacyproto.ListColumnBlack,
		Name:   "url.default.black",
	}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yacyproto.PathList+"?"+form.Encode(),
		nil,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "example.org/.*\r\nblocked.example/.*\r\n" {
		t.Fatalf("Body = %q", rec.Body.String())
	}
}
