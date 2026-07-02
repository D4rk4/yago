package sharedblacklist

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

type recordingBlacklists struct {
	name string
}

func (r *recordingBlacklists) SharedList(_ context.Context, name string) string {
	r.name = name

	return "example.org/.*\r\nblocked.example/.*\r\n"
}

func TestListExportsSharedBlacklistEntries(t *testing.T) {
	blacklists := &recordingBlacklists{}
	resp, err := endpoint{networkName: "freeworld", blacklists: blacklists}.Serve(
		t.Context(),
		yacyproto.ListRequest{
			NetworkName: "freeworld",
			Column:      yacyproto.ListColumnBlack,
			Name:        "url.default.black",
		},
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
	resp, err := endpoint{networkName: "freeworld", blacklists: blacklists}.Serve(
		t.Context(),
		yacyproto.ListRequest{
			NetworkName: "freeworld",
			Column:      "blue",
			Name:        "url.default.black",
		},
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

func TestListRejectsForeignNetwork(t *testing.T) {
	blacklists := &recordingBlacklists{}
	resp, err := endpoint{networkName: "freeworld", blacklists: blacklists}.Serve(
		t.Context(),
		yacyproto.ListRequest{
			NetworkName: "othernet",
			Column:      yacyproto.ListColumnBlack,
			Name:        "url.default.black",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Body != "" {
		t.Fatalf("Body = %q, want empty auth failure response", resp.Body)
	}
	if blacklists.name != "" {
		t.Fatalf("blacklists consulted for foreign network: %q", blacklists.name)
	}
}

func TestNoSharedBlacklistsReturnsNoEntries(t *testing.T) {
	blacklists := NoSharedBlacklists{}
	if got := blacklists.SharedList(t.Context(), "url.default.black"); got != "" {
		t.Fatalf("list = %q, want empty", got)
	}
}

func TestMountServesSharedBlacklistRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	blacklists := &recordingBlacklists{}
	Mount(router, "freeworld", blacklists)
	form := yacyproto.ListRequest{
		NetworkName: "freeworld",
		Column:      yacyproto.ListColumnBlack,
		Name:        "url.default.black",
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

func TestFileBlacklistsReadsSharedLists(t *testing.T) {
	blacklists := FileBlacklists{files: fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte(
			"# ignored\n" +
				"BlackLists.Shared = url.default.black, url.other.black\n",
		)},
		"LISTS/url.default.black": {Data: []byte(
			"# ignored\n" +
				"example.org/.*\n" +
				"\n" +
				"blocked.example/.*\n",
		)},
		"LISTS/url.other.black": {Data: []byte("other.example/.*\n")},
	}}

	got := blacklists.SharedList(t.Context(), "url.default.black")
	want := "example.org/.*\r\nblocked.example/.*\r\n\r\n"
	if got != want {
		t.Fatalf("list = %q, want %q", got, want)
	}

	got = blacklists.SharedList(t.Context(), "")
	want = "example.org/.*\r\nblocked.example/.*\r\n\r\nother.example/.*\r\n\r\n"
	if got != want {
		t.Fatalf("all lists = %q, want %q", got, want)
	}
}

func TestNewFileBlacklistsReadsDataDirectoryLists(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "SETTINGS"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "LISTS"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dataDir, "SETTINGS", "yacy.conf"),
		[]byte("BlackLists.Shared=url.default.black\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dataDir, "LISTS", "url.default.black"),
		[]byte("example.org/.*\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	got := NewFileBlacklists(dataDir).SharedList(t.Context(), "url.default.black")
	if got != "example.org/.*\r\n\r\n" {
		t.Fatalf("list = %q", got)
	}
}

func TestFileBlacklistsReturnsEmptyWhenUnconfiguredOrCanceled(t *testing.T) {
	blacklists := FileBlacklists{files: fstest.MapFS{}}
	if got := blacklists.SharedList(t.Context(), "url.default.black"); got != "" {
		t.Fatalf("missing config list = %q, want empty", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blacklists = FileBlacklists{files: fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte("BlackLists.Shared=url.default.black\n")},
	}}
	if got := blacklists.SharedList(ctx, "url.default.black"); got != "" {
		t.Fatalf("canceled list = %q, want empty", got)
	}
}

func TestFileBlacklistsSkipsUnsharedAndUnsafeNames(t *testing.T) {
	blacklists := FileBlacklists{files: fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte(
			"BlackLists.Shared=url.default.black,../secret,sub/dir\n",
		)},
		"LISTS/url.default.black": {Data: []byte("example.org/.*\n")},
	}}

	if got := blacklists.SharedList(t.Context(), "not.shared"); got != "" {
		t.Fatalf("unshared list = %q, want empty", got)
	}
	if got := blacklists.SharedList(
		t.Context(),
		"url.default.black",
	); got != "example.org/.*\r\n\r\n" {
		t.Fatalf("safe list = %q", got)
	}
	if got := blacklists.SharedList(t.Context(), ""); got != "example.org/.*\r\n\r\n" {
		t.Fatalf("all shared lists = %q", got)
	}
	if got := blacklists.listText(t.Context(), "../secret"); got != "" {
		t.Fatalf("unsafe direct list = %q, want empty", got)
	}
}

func TestFileBlacklistsReturnsBlankForMissingSharedFile(t *testing.T) {
	blacklists := FileBlacklists{files: fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte("BlackLists.Shared=missing.black\n")},
	}}
	if got := blacklists.SharedList(t.Context(), "missing.black"); got != "\r\n" {
		t.Fatalf("missing list = %q, want CRLF", got)
	}
}

func TestFileBlacklistsReturnsEmptyOnReadErrors(t *testing.T) {
	blacklists := FileBlacklists{files: fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte("BlackLists.Shared=url.default.black\n")},
		"LISTS/url.default.black":     {Mode: fs.ModeDir},
	}}
	if got := blacklists.SharedList(t.Context(), "url.default.black"); got != "\r\n" {
		t.Fatalf("file error list = %q, want CRLF", got)
	}

	blacklists = FileBlacklists{files: fstest.MapFS{
		sharedBlacklistConfigFileName: {Mode: fs.ModeDir},
	}}
	if got := blacklists.SharedList(t.Context(), "url.default.black"); got != "" {
		t.Fatalf("config error list = %q, want empty", got)
	}
}

func TestSettingValueParsesYaCyEscapes(t *testing.T) {
	raw := "Other=x\nBlackLists.Shared=url.default.black\\,ignored,url\\=second.black\n"
	if got := settingValue(
		raw,
		sharedBlacklistConfigKey,
	); got != `url.default.black\,ignored,url=second.black` {
		t.Fatalf("setting = %q", got)
	}
	if got := settingValue("bad\nkey=value\n", "missing"); got != "" {
		t.Fatalf("missing setting = %q", got)
	}
	if got := settingValue(
		`BlackLists\.Shared=wrong`+"\n"+`BlackLists.Shared=a\nb\\c`+"\n",
		sharedBlacklistConfigKey,
	); got != "a\nb\\c" {
		t.Fatalf("escaped setting = %q", got)
	}
	if got := settingSeparator(`a\=b=c`); got != 4 {
		t.Fatalf("separator = %d, want 4", got)
	}
}

func TestSharedBlacklistTextKeepsNonCommentLines(t *testing.T) {
	got := sharedBlacklistText("# ignored\n value\n!kept\n\n")
	want := " value\r\n!kept\r\n"
	if got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}
