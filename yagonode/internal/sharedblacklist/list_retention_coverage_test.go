package sharedblacklist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

var errScriptedBlacklistRead = errors.New("scripted blacklist read failure")

type openFailureBlacklistFS struct {
	fs.FS
	target string
}

func (f openFailureBlacklistFS) Open(name string) (fs.File, error) {
	if name == f.target {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open fallback blacklist file: %w", err)
	}

	return file, nil
}

type readFailureBlacklistFS struct {
	fs.FS
	target string
}

func (f readFailureBlacklistFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open read-failure blacklist file: %w", err)
	}
	if name != f.target {
		return file, nil
	}

	return &readFailureBlacklistFile{File: file}, nil
}

type readFailureBlacklistFile struct {
	fs.File
	once sync.Once
}

func (f *readFailureBlacklistFile) Read(destination []byte) (int, error) {
	read, err := f.File.Read(destination)
	failed := false
	f.once.Do(func() { failed = true })
	if failed && read > 0 {
		return read, errScriptedBlacklistRead
	}
	if errors.Is(err, io.EOF) {
		return read, io.EOF
	}
	if err != nil {
		return read, fmt.Errorf("read fallback blacklist file: %w", err)
	}

	return read, nil
}

type cancelOnCloseBlacklistFS struct {
	fs.FS
	target string
	cancel context.CancelFunc
}

func (f cancelOnCloseBlacklistFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open cancel-on-close blacklist file: %w", err)
	}
	if name != f.target {
		return file, nil
	}

	return &cancelOnCloseBlacklistFile{File: file, cancel: f.cancel}, nil
}

type cancelOnCloseBlacklistFile struct {
	fs.File
	cancel context.CancelFunc
}

func (f *cancelOnCloseBlacklistFile) Close() error {
	f.cancel()
	if err := f.File.Close(); err != nil {
		return fmt.Errorf("close cancel-on-close blacklist file: %w", err)
	}

	return nil
}

func sharedBlacklistMapFS(config, content string) fstest.MapFS {
	return fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte(config)},
		"LISTS/a":                     {Data: []byte(content)},
	}
}

func TestFileBlacklistsControlledOpenAndReadFailures(t *testing.T) {
	base := sharedBlacklistMapFS("BlackLists.Shared=a\n", "entry\n")
	configOpen := FileBlacklists{files: openFailureBlacklistFS{
		FS: base, target: sharedBlacklistConfigFileName,
	}}
	if list, err := configOpen.sharedList(t.Context(), "a"); list != "" || err != nil {
		t.Fatalf("config open failure = %q err=%v", list, err)
	}

	listOpen := FileBlacklists{files: openFailureBlacklistFS{FS: base, target: "LISTS/a"}}
	if got := listOpen.SharedList(t.Context(), "a"); got != sharedBlacklistLineBreak {
		t.Fatalf("list open failure = %q", got)
	}

	configReadBase := sharedBlacklistMapFS("Other=x\n", "entry\n")
	configRead := FileBlacklists{files: readFailureBlacklistFS{
		FS: configReadBase, target: sharedBlacklistConfigFileName,
	}}
	if list, err := configRead.sharedList(t.Context(), "a"); list != "" || err != nil {
		t.Fatalf("config read failure = %q err=%v", list, err)
	}

	listRead := FileBlacklists{files: readFailureBlacklistFS{FS: base, target: "LISTS/a"}}
	if list, err := listRead.sharedList(t.Context(), "a"); list != "" ||
		!errors.Is(err, errScriptedBlacklistRead) {
		t.Fatalf("list read failure = %q err=%v", list, err)
	}
}

func TestFileBlacklistsContextAndBudgetFailurePoints(t *testing.T) {
	config := "BlackLists.Shared=a\n"
	base := sharedBlacklistMapFS(config, "abcdef")
	ctx, cancelAfterConfig := context.WithCancel(context.Background())
	t.Cleanup(cancelAfterConfig)
	afterConfig := FileBlacklists{files: cancelOnCloseBlacklistFS{
		FS: base, target: sharedBlacklistConfigFileName, cancel: cancelAfterConfig,
	}}
	if list, err := afterConfig.sharedList(ctx, "a"); list != "" ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("post-config cancellation = %q err=%v", list, err)
	}

	ctx, cancelDuringConfig := context.WithCancel(context.Background())
	t.Cleanup(cancelDuringConfig)
	duringConfig := FileBlacklists{files: cancelingBlacklistFS{
		FS: base, target: sharedBlacklistConfigFileName, cancel: cancelDuringConfig,
	}}
	if list, err := duringConfig.sharedList(ctx, "a"); list != "" ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("config cancellation = %q err=%v", list, err)
	}

	matchingLineOverflow := FileBlacklists{files: base, maximumAggregateBytes: len(config)}
	if _, err := matchingLineOverflow.sharedList(
		t.Context(),
		"a",
	); !errors.Is(
		err,
		errSharedBlacklistBudgetExceeded,
	) {
		t.Fatalf("matching line overflow err=%v", err)
	}

	configInputOverflow := FileBlacklists{files: base, maximumAggregateBytes: 3}
	if _, err := configInputOverflow.sharedList(
		t.Context(),
		"a",
	); !errors.Is(
		err,
		errSharedBlacklistBudgetExceeded,
	) {
		t.Fatalf("config input overflow err=%v", err)
	}
}

func TestFileBlacklistsListInputAndNameRetentionOverflow(t *testing.T) {
	config := "BlackLists.Shared=a\n"
	baseRetention := len(config)*2 + retainedSharedBlacklistNameBytes + 1
	limited := FileBlacklists{
		files:                 sharedBlacklistMapFS(config, "abcdef"),
		maximumAggregateBytes: baseRetention + 3,
	}
	if _, err := limited.sharedList(
		t.Context(),
		"a",
	); !errors.Is(
		err,
		errSharedBlacklistBudgetExceeded,
	) {
		t.Fatalf("list input overflow err=%v", err)
	}
	commentLimited := FileBlacklists{
		files:                 sharedBlacklistMapFS(config, "#abcdef"),
		maximumAggregateBytes: baseRetention + 3,
	}
	if _, err := commentLimited.sharedList(
		t.Context(),
		"a",
	); !errors.Is(
		err,
		errSharedBlacklistBudgetExceeded,
	) {
		t.Fatalf("comment input overflow err=%v", err)
	}
	directLimited := FileBlacklists{files: limited.files, maximumAggregateBytes: 3}
	if got := directLimited.listText(t.Context(), "a"); got != "" {
		t.Fatalf("limited direct list = %q", got)
	}

	retention := newSharedBlacklistRetention(retainedSharedBlacklistNameBytes)
	if names, err := retainedSharedBlacklistNames("a", retention); len(names) != 0 ||
		!errors.Is(err, errSharedBlacklistBudgetExceeded) {
		t.Fatalf("name retention = %v err=%v", names, err)
	}
}

func TestFileBlacklistsEmptyConfigurationAndReaderCancellation(t *testing.T) {
	blacklists := FileBlacklists{files: fstest.MapFS{
		sharedBlacklistConfigFileName: {Data: []byte("Other=x\n")},
	}}
	if list, err := blacklists.sharedList(t.Context(), ""); list != "" || err != nil {
		t.Fatalf("empty configuration = %q err=%v", list, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	retention := newSharedBlacklistRetention(1)
	_, err := sharedBlacklistReader{
		ctx: ctx, source: strings.NewReader("x"), retention: retention,
	}.Read(make([]byte, 1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reader err=%v", err)
	}
}

func TestSharedBlacklistTextRejectsInputAndOutputOverflow(t *testing.T) {
	tooLarge := strings.Repeat("x", maximumSharedBlacklistAggregateBytes+1)
	if got := sharedBlacklistText(tooLarge); got != "" {
		t.Fatalf("oversized input retained %d bytes", len(got))
	}
	outputOverflow := strings.Repeat("x", maximumSharedBlacklistAggregateBytes/2+1)
	if got := sharedBlacklistText(outputOverflow); got != "" {
		t.Fatalf("oversized output retained %d bytes", len(got))
	}
}
