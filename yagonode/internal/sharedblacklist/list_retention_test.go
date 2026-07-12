package sharedblacklist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type sharedBlacklistBodyProbe struct {
	reads atomic.Int64
}

func (p *sharedBlacklistBodyProbe) Read([]byte) (int, error) {
	p.reads.Add(1)

	return 0, io.EOF
}

func (*sharedBlacklistBodyProbe) Close() error { return nil }

type blockingSharedBlacklists struct {
	started chan struct{}
	unblock <-chan struct{}
}

func (b blockingSharedBlacklists) SharedList(ctx context.Context, _ string) string {
	select {
	case b.started <- struct{}{}:
	case <-ctx.Done():
		return ""
	}
	select {
	case <-b.unblock:
		return "example.org/.*\r\n"
	case <-ctx.Done():
		return ""
	}
}

type cancelingBlacklistFS struct {
	fs.FS
	target string
	cancel context.CancelFunc
}

func (c cancelingBlacklistFS) Open(name string) (fs.File, error) {
	file, err := c.FS.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open canceling blacklist file: %w", err)
	}
	if name != c.target {
		return file, nil
	}

	return &cancelingBlacklistFile{File: file, cancel: c.cancel}, nil
}

type cancelingBlacklistFile struct {
	fs.File
	cancel context.CancelFunc
	once   sync.Once
}

func (f *cancelingBlacklistFile) Read(destination []byte) (int, error) {
	read, err := f.File.Read(destination)
	f.once.Do(f.cancel)
	if errors.Is(err, io.EOF) {
		return read, io.EOF
	}
	if err != nil {
		return read, fmt.Errorf("read canceling blacklist file: %w", err)
	}

	return read, nil
}

func sharedBlacklistHandler(
	t *testing.T,
	blacklists Blacklists,
) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, 5*time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	Mount(router, "freeworld", blacklists)

	return mux
}

func sharedBlacklistRequestForm() string {
	return yagoproto.ListRequest{
		NetworkName: "freeworld",
		Column:      yagoproto.ListColumnBlack,
		Name:        "url.default.black",
	}.Form().Encode()
}

func TestSharedBlacklistAdmissionShedsBeforeBodyAndRecovers(t *testing.T) {
	unblock := make(chan struct{})
	var unblockOnce sync.Once
	release := func() { unblockOnce.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	blacklists := blockingSharedBlacklists{
		started: make(chan struct{}, maximumConcurrentSharedBlacklist+1),
		unblock: unblock,
	}
	handler := sharedBlacklistHandler(t, blacklists)
	completed := make(chan *httptest.ResponseRecorder, maximumConcurrentSharedBlacklist)
	form := sharedBlacklistRequestForm()
	for range maximumConcurrentSharedBlacklist {
		request := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			yagoproto.PathList,
			strings.NewReader(form),
		)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		go func() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			completed <- recorder
		}()
	}
	for range maximumConcurrentSharedBlacklist {
		select {
		case <-blacklists.started:
		case <-time.After(10 * time.Second):
			t.Fatal("shared blacklist request did not reach provider")
		}
	}

	probe := &sharedBlacklistBodyProbe{}
	overflowRequest := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yagoproto.PathList,
		nil,
	)
	overflowRequest.Body = probe
	overflowRequest.ContentLength = -1
	overflowRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	overflow := httptest.NewRecorder()
	handler.ServeHTTP(overflow, overflowRequest)
	if overflow.Code != http.StatusServiceUnavailable ||
		overflow.Header().Get("Retry-After") != "1" || probe.reads.Load() != 0 {
		t.Fatalf(
			"overflow = %d retry=%q reads=%d",
			overflow.Code,
			overflow.Header().Get("Retry-After"),
			probe.reads.Load(),
		)
	}

	release()
	for range maximumConcurrentSharedBlacklist {
		if recorder := <-completed; recorder.Code != http.StatusOK {
			t.Fatalf("blocked response = %d", recorder.Code)
		}
	}
	recovery := httptest.NewRecorder()
	handler.ServeHTTP(recovery, httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathList+"?"+form,
		nil,
	))
	if recovery.Code != http.StatusOK || recovery.Body.String() != "example.org/.*\r\n" {
		t.Fatalf("recovery = %d %q", recovery.Code, recovery.Body.String())
	}
}

func sharedBlacklistFixtureBudget(config string, names []string, contents ...string) int {
	retained := len(config) * 2
	for _, name := range names {
		retained += retainedSharedBlacklistNameBytes + len(name)
	}
	for _, content := range contents {
		retained += len(content) + len(sharedBlacklistText(content)) + len(sharedBlacklistLineBreak)
	}

	return retained
}

func TestFileBlacklistsAcceptsExactAggregateAndRejectsPlusOne(t *testing.T) {
	config := "BlackLists.Shared=a\n"
	content := "entry\n"
	files := fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte(config)},
		"LISTS/a":                     {Data: []byte(content)},
	}
	maximum := sharedBlacklistFixtureBudget(config, []string{"a"}, content)
	exact := FileBlacklists{files: files, maximumAggregateBytes: maximum}
	list, err := exact.sharedList(t.Context(), "a")
	if err != nil || list != "entry\r\n\r\n" {
		t.Fatalf("exact aggregate = %q err=%v", list, err)
	}

	overflow := FileBlacklists{files: files, maximumAggregateBytes: maximum - 1}
	list, err = overflow.sharedList(t.Context(), "a")
	if list != "" || !errors.Is(err, errSharedBlacklistBudgetExceeded) ||
		overflow.SharedList(t.Context(), "a") != "" {
		t.Fatalf("overflow aggregate = %q err=%v", list, err)
	}
}

func TestFileBlacklistsSharesAggregateAcrossFiles(t *testing.T) {
	config := "BlackLists.Shared=a,b\n"
	first, second := "one\n", "two\n"
	files := fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte(config)},
		"LISTS/a":                     {Data: []byte(first)},
		"LISTS/b":                     {Data: []byte(second)},
	}
	maximum := sharedBlacklistFixtureBudget(config, []string{"a", "b"}, first, second)
	exact := FileBlacklists{files: files, maximumAggregateBytes: maximum}
	if got := exact.SharedList(t.Context(), ""); got != "one\r\n\r\ntwo\r\n\r\n" {
		t.Fatalf("combined exact list = %q", got)
	}

	firstOnly := sharedBlacklistFixtureBudget(config, []string{"a", "b"}, first)
	limited := FileBlacklists{files: files, maximumAggregateBytes: firstOnly}
	if got := limited.SharedList(t.Context(), ""); got != "" {
		t.Fatalf("combined overflow returned partial list %q", got)
	}
}

func TestSharedBlacklistReaderExactBoundaryAndPlusOne(t *testing.T) {
	exactRetention := newSharedBlacklistRetention(3)
	exact, err := io.ReadAll(sharedBlacklistReader{
		ctx:       t.Context(),
		source:    strings.NewReader("abc"),
		retention: exactRetention,
	})
	if err != nil || string(exact) != "abc" || exactRetention.remaining != 0 {
		t.Fatalf("exact read = %q remaining=%d err=%v", exact, exactRetention.remaining, err)
	}

	overflowRetention := newSharedBlacklistRetention(3)
	_, err = io.ReadAll(sharedBlacklistReader{
		ctx:       t.Context(),
		source:    strings.NewReader("abcd"),
		retention: overflowRetention,
	})
	if !errors.Is(err, errSharedBlacklistBudgetExceeded) || overflowRetention.remaining != 0 {
		t.Fatalf("overflow read remaining=%d err=%v", overflowRetention.remaining, err)
	}
}

func TestFileBlacklistsCancellationDiscardsPartialResponse(t *testing.T) {
	base := fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte("BlackLists.Shared=a\n")},
		"LISTS/a":                     {Data: []byte("first\nsecond\n")},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	blacklists := FileBlacklists{files: cancelingBlacklistFS{
		FS:     base,
		target: "LISTS/a",
		cancel: cancel,
	}}
	list, err := blacklists.sharedList(ctx, "a")
	if list != "" || !errors.Is(err, context.Canceled) || blacklists.SharedList(ctx, "a") != "" {
		t.Fatalf("canceled list = %q err=%v", list, err)
	}
}

func TestRetainedSharedBlacklistNamesRejectsEntryOverflow(t *testing.T) {
	names := strings.Repeat("a,", maximumSharedBlacklistFiles) + "a"
	retention := newSharedBlacklistRetention(maximumSharedBlacklistAggregateBytes)
	retained, err := retainedSharedBlacklistNames(names, retention)
	if len(retained) != maximumSharedBlacklistFiles ||
		!errors.Is(err, errSharedBlacklistBudgetExceeded) {
		t.Fatalf("retained names = %d err=%v", len(retained), err)
	}
}
