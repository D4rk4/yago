package portaltheme_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/portaltheme"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type recordedEvent struct {
	severity events.Severity
	category events.Category
	name     string
	message  string
}

type captureSink struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (s *captureSink) Record(
	severity events.Severity,
	category events.Category,
	name, message string,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, recordedEvent{severity, category, name, message})
}

func (s *captureSink) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.events))
	for _, event := range s.events {
		if event.name != "portal.theme" ||
			event.severity != events.SeverityInfo ||
			event.category != events.CategoryConfig {
			out = append(out, "UNEXPECTED:"+event.name)

			continue
		}
		out = append(out, event.message)
	}

	return out
}

func openTheme(t *testing.T) (*portaltheme.Theme, *captureSink) {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	sink := &captureSink{}
	theme, err := portaltheme.Open(v, sink)
	if err != nil {
		t.Fatalf("open theme: %v", err)
	}

	return theme, sink
}

func TestThemeStartsDisabledAndFallsBack(t *testing.T) {
	theme, _ := openTheme(t)

	if theme.Enabled() {
		t.Fatal("a fresh theme must start disabled")
	}
	if _, ok := theme.Render(context.Background(), portaltheme.PageSearch, map[string]any{}); ok {
		t.Fatal("a fresh theme must not render")
	}
}

func TestThemeRendersSavedTemplateWhenEnabled(t *testing.T) {
	theme, sink := openTheme(t)
	ctx := context.Background()

	if err := theme.SetEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	doc, err := theme.SaveDocument(
		ctx,
		portaltheme.PageSearch,
		`<html><style>{{{styles}}}</style><h1>{{brand}}</h1><p>{{query}}</p></html>`,
	)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !doc.ParseOK || doc.ParseError != "" {
		t.Fatalf("healthy body reported unparsed: %+v", doc)
	}
	if _, err := theme.SaveDocument(
		ctx,
		portaltheme.SharedStyles,
		"body { color: red; }",
	); err != nil {
		t.Fatalf("save styles: %v", err)
	}

	html, ok := theme.Render(context.Background(), portaltheme.PageSearch, map[string]any{
		"brand": "my node",
		"query": `<script>alert(1)</script>`,
	})
	if !ok {
		t.Fatal("enabled theme with a healthy template must render")
	}
	if !strings.Contains(html, "<h1>my node</h1>") {
		t.Errorf("brand not interpolated: %s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") || strings.Contains(html, "<script>alert") {
		t.Errorf("query must render escaped: %s", html)
	}
	if !strings.Contains(html, "<style>body { color: red; }</style>") {
		t.Errorf("shared styles must inject raw: %s", html)
	}

	messages := sink.messages()
	if len(messages) != 3 {
		t.Fatalf("events = %q, want enable + two saves", messages)
	}
	if messages[0] != "operator portal theme enabled" {
		t.Errorf("enable event = %q", messages[0])
	}
	if !strings.Contains(messages[1], `"search" saved`) {
		t.Errorf("save event = %q", messages[1])
	}
}

func TestThemeRenderRequiresEnabledToggle(t *testing.T) {
	theme, _ := openTheme(t)
	ctx := context.Background()

	if _, err := theme.SaveDocument(ctx, portaltheme.PageResults, "<p>{{query}}</p>"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, ok := theme.Render(context.Background(), portaltheme.PageResults, map[string]any{}); ok {
		t.Fatal("a disabled theme must not render")
	}
	if err := theme.SetEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, ok := theme.Render(context.Background(), portaltheme.PageResults, map[string]any{}); !ok {
		t.Fatal("enabling must activate the stored template")
	}
	if err := theme.SetEnabled(ctx, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, ok := theme.Render(context.Background(), portaltheme.PageResults, map[string]any{}); ok {
		t.Fatal("disabling must deactivate the stored template")
	}
}

func TestThemeKeepsUnparseableBodyAndFallsBack(t *testing.T) {
	theme, _ := openTheme(t)
	ctx := context.Background()

	if err := theme.SetEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	doc, err := theme.SaveDocument(ctx, portaltheme.PageSearch, "{{#if}}")
	if err != nil {
		t.Fatalf("an unparseable body must still be stored: %v", err)
	}
	if doc.ParseOK || doc.ParseError == "" {
		t.Fatalf("parse status not recorded: %+v", doc)
	}
	stored, found, err := theme.Document(ctx, portaltheme.PageSearch)
	if err != nil || !found {
		t.Fatalf("stored document lost: %v found=%v", err, found)
	}
	if stored.ParseOK || stored.Body != "{{#if}}" {
		t.Fatalf("stored document mismatch: %+v", stored)
	}
	if _, ok := theme.Render(context.Background(), portaltheme.PageSearch, map[string]any{}); ok {
		t.Fatal("an unparseable template must fall back")
	}
}

func TestThemeStylesBodySkipsHandlebarsParsing(t *testing.T) {
	theme, _ := openTheme(t)

	doc, err := theme.SaveDocument(
		context.Background(),
		portaltheme.SharedStyles,
		"a::after { content: '{{'; }",
	)
	if err != nil {
		t.Fatalf("save styles: %v", err)
	}
	if !doc.ParseOK {
		t.Fatalf("styles must not be parsed as a template: %+v", doc)
	}
}

func TestThemeRejectsOversizedDocument(t *testing.T) {
	theme, _ := openTheme(t)

	_, err := theme.SaveDocument(
		context.Background(),
		portaltheme.PageSearch,
		strings.Repeat("x", portaltheme.MaxDocumentBytes+1),
	)
	if err == nil || !strings.Contains(err.Error(), "byte cap") {
		t.Fatalf("oversized body must be rejected, got %v", err)
	}
}

func TestThemeRejectsUnknownDocument(t *testing.T) {
	theme, _ := openTheme(t)
	ctx := context.Background()

	if _, err := theme.SaveDocument(ctx, "sidebar", "x"); err == nil {
		t.Error("save must reject an unknown document")
	}
	if _, _, err := theme.Document(ctx, "sidebar"); err == nil {
		t.Error("load must reject an unknown document")
	}
	if _, err := theme.ResetDocument(ctx, "sidebar"); err == nil {
		t.Error("reset must reject an unknown document")
	}
	if _, ok := theme.Render(context.Background(), "sidebar", map[string]any{}); ok {
		t.Error("render must decline an unknown document")
	}
}

func TestThemeResetDropsOverride(t *testing.T) {
	theme, sink := openTheme(t)
	ctx := context.Background()

	if err := theme.SetEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := theme.SaveDocument(ctx, portaltheme.PageResults, "<p>themed</p>"); err != nil {
		t.Fatalf("save: %v", err)
	}
	existed, err := theme.ResetDocument(ctx, portaltheme.PageResults)
	if err != nil || !existed {
		t.Fatalf("reset existing = %v, %v", existed, err)
	}
	if _, ok := theme.Render(context.Background(), portaltheme.PageResults, map[string]any{}); ok {
		t.Fatal("a reset page must fall back")
	}
	if _, _, err := theme.Document(ctx, portaltheme.PageResults); err != nil {
		t.Fatalf("document after reset: %v", err)
	}
	existed, err = theme.ResetDocument(ctx, portaltheme.PageResults)
	if err != nil || existed {
		t.Fatalf("reset absent = %v, %v", existed, err)
	}
	if got := sink.messages(); !strings.Contains(strings.Join(got, "|"), "reset to default") {
		t.Errorf("reset event missing: %q", got)
	}
}

func TestThemeHelpers(t *testing.T) {
	theme, _ := openTheme(t)
	ctx := context.Background()

	if err := theme.SetEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	body := `{{urlencode q}}|{{truncate long 3}}|{{truncate long 99}}|{{truncate long -1}}` +
		`|{{pluralize one "result" "results"}}|{{pluralize many "result" "results"}}` +
		`|{{formatNumber big}}|{{formatNumber negative}}|{{formatNumber small}}`
	if _, err := theme.SaveDocument(ctx, portaltheme.PageSearch, body); err != nil {
		t.Fatalf("save: %v", err)
	}
	html, ok := theme.Render(context.Background(), portaltheme.PageSearch, map[string]any{
		"q":        "a b&c",
		"long":     "abcdef",
		"one":      1,
		"many":     2,
		"big":      1234567,
		"negative": -1234,
		"small":    987,
	})
	if !ok {
		t.Fatal("helper template must render")
	}
	want := "a+b%26c|abc…|abcdef|…|result|results|1 234 567|-1 234|987"
	if html != want {
		t.Errorf("helpers rendered %q, want %q", html, want)
	}
}

func TestThemeDefaultBodiesParseAndRender(t *testing.T) {
	theme, _ := openTheme(t)
	ctx := context.Background()

	if err := theme.SetEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	for _, page := range []string{portaltheme.PageSearch, portaltheme.PageResults} {
		doc, err := theme.SaveDocument(ctx, page, portaltheme.DefaultBody(page))
		if err != nil {
			t.Fatalf("save default %s: %v", page, err)
		}
		if !doc.ParseOK {
			t.Fatalf("default %s body must parse: %s", page, doc.ParseError)
		}
	}
	if _, err := theme.SaveDocument(
		ctx,
		portaltheme.SharedStyles,
		portaltheme.DefaultBody(portaltheme.SharedStyles),
	); err != nil {
		t.Fatalf("save default styles: %v", err)
	}
	if portaltheme.DefaultBody("sidebar") != "" {
		t.Error("unknown page must have no default body")
	}

	html, ok := theme.Render(context.Background(), portaltheme.PageResults, resultsView())
	if !ok {
		t.Fatal("default results body must render")
	}
	assertContainsAll(t, "default results", html, []string{
		"<title>cats — my node search</title>",
		"2 results for “cats” (0.42 s)",
		"1 from this node · 1 from peers · 0 from the web",
		`href="https://example.com/a" target="_blank" rel="noopener noreferrer nofollow">First hit<span aria-hidden="true"> ↗</span>`,
		"escaped &lt;mark&gt;snippet",
		"<mark>real</mark>",
		`<span class="prov prov-local">local</span>`,
		`<a rel="next" href="/?p=2&amp;q=cats">Next ›</a>`,
		"Did you mean",
		"showing close matches",
		`<aside class="facets"`,
		`<div class="brand"><b>ya</b>go</div>`,
		"free software under the GNU AGPL v3",
		"body { font-family: Arial, Helvetica, sans-serif;",
		`<a href="/yacysearch.rss?query=cats">RSS</a>`,
	})
	imageView := resultsView()
	imageView["imageVertical"] = true
	grid, ok := theme.Render(context.Background(), portaltheme.PageResults, imageView)
	if !ok {
		t.Fatal("default results body must render the image vertical")
	}
	assertContainsAll(t, "default image grid", grid, []string{
		`<ul class="imggrid">`,
		`<a href="#img-0-0"><img src="/img?u=1" alt="an image" loading="lazy"></a>`,
		`<div class="lightbox" id="img-0-0">`,
		`From <a href="https://example.com/a" rel="noreferrer nofollow">First hit</a>`,
	})

	home, ok := theme.Render(context.Background(), portaltheme.PageSearch, map[string]any{
		"brand": "my node",
		"query": "",
	})
	if !ok {
		t.Fatal("default search body must render")
	}
	assertContainsAll(t, "default search", home, []string{
		"<title>my node search</title>",
		`<div class="brand"><b>ya</b>go</div>`,
		"Search operators",
		`"quoted phrase"`,
		"prefer results where the words appear adjacently",
		`<div class="home">`,
	})
}

func TestThemeDefaultResultsRenderTotalMissSuggestion(t *testing.T) {
	theme, _ := openTheme(t)
	if err := theme.SetEnabled(t.Context(), true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := theme.SaveDocument(
		t.Context(),
		portaltheme.PageResults,
		portaltheme.DefaultBody(portaltheme.PageResults),
	); err != nil {
		t.Fatalf("save default results: %v", err)
	}
	missView := resultsView()
	missResults, _ := missView["results"].(map[string]any)
	missResults["recovered"] = false
	missResults["totalResults"] = 0
	missResults["results"] = []map[string]any{}
	miss, ok := theme.Render(t.Context(), portaltheme.PageResults, missView)
	if !ok || !strings.Contains(miss, "No results matched. Did you mean") ||
		strings.Contains(miss, "showing close matches") {
		t.Fatalf("total-miss suggestion render = %q", miss)
	}
}

func assertContainsAll(t *testing.T, label, html string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("%s render misses %q", label, want)
		}
	}
}

func resultsView() map[string]any {
	return map[string]any{
		"brand":         "my node",
		"query":         "cats",
		"imageVertical": false,
		"submitted":     true,
		"error":         "",
		"newTab":        true,
		"rssUrl":        "/yacysearch.rss?query=cats",
		"jsonUrl":       "/yacysearch.json?query=cats",
		"elapsed":       "0.42 s",
		"verticals": []map[string]any{
			{"label": "All", "url": "/?q=cats", "current": true},
		},
		"results": map[string]any{
			"query":         "cats",
			"totalResults":  2,
			"localCount":    1,
			"peerCount":     1,
			"webCount":      0,
			"recovered":     true,
			"didYouMean":    "cat",
			"didYouMeanUrl": "/?q=cat",
			"facets": []map[string]any{
				{
					"title": "Hosts",
					"items": []map[string]any{
						{"label": "example.com", "count": 5, "url": "/?q=cats+site%3Aexample.com"},
					},
				},
			},
			"results": []map[string]any{
				{
					"title":       "First hit",
					"url":         "https://example.com/a",
					"displayUrl":  "example.com/a",
					"snippet":     "",
					"snippetHtml": "escaped &lt;mark&gt;snippet with a <mark>real</mark> mark",
					"provenance":  "local",
					"date":        "2026-07-01",
					"cachedUrl":   "/cache?u=1",
					"images": []map[string]any{
						{
							"proxyUrl": "/img?u=1",
							"alt":      "an image",
							"pageUrl":  "https://example.com/a",
						},
					},
				},
				{
					"title":       "Second hit",
					"url":         "https://example.com/b",
					"displayUrl":  "example.com/b",
					"snippet":     "plain snippet",
					"snippetHtml": "",
					"provenance":  "peer",
				},
			},
		},
		"pagination": map[string]any{
			"show":    true,
			"page":    1,
			"hasPrev": false,
			"hasNext": true,
			"nextUrl": "/?p=2&q=cats",
			"pages": []map[string]any{
				{"number": 1, "url": "/?p=1&q=cats", "current": true},
				{"number": 2, "url": "/?p=2&q=cats", "current": false},
			},
		},
	}
}

func TestThemeSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	v, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("boltvault: %v", err)
	}
	ctx := context.Background()
	theme, err := portaltheme.Open(v, &captureSink{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := theme.SetEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := theme.SaveDocument(ctx, portaltheme.PageSearch, "<p>{{brand}}</p>"); err != nil {
		t.Fatalf("save page: %v", err)
	}
	if _, err := theme.SaveDocument(ctx, portaltheme.SharedStyles, "b{}"); err != nil {
		t.Fatalf("save styles: %v", err)
	}
	if _, err := theme.SaveDocument(ctx, portaltheme.PageResults, "{{#if}}"); err != nil {
		t.Fatalf("save broken page: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("close vault: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	restored, err := portaltheme.Open(reopened, &captureSink{})
	if err != nil {
		t.Fatalf("reopen theme: %v", err)
	}
	if !restored.Enabled() {
		t.Fatal("enabled flag must survive a restart")
	}
	html, ok := restored.Render(
		context.Background(),
		portaltheme.PageSearch,
		map[string]any{"brand": "back"},
	)
	if !ok || !strings.Contains(html, "<p>back</p>") {
		t.Fatalf("stored template must survive a restart: %q %v", html, ok)
	}
	if _, ok := restored.Render(
		context.Background(),
		portaltheme.PageResults,
		map[string]any{},
	); ok {
		t.Fatal("a stored unparseable template must keep falling back after restart")
	}
}

func TestThemeOpenRejectsConflictingBuckets(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	defer func() { _ = v.Close() }()
	if _, err := portaltheme.Open(v, &captureSink{}); err != nil {
		t.Fatalf("first open: %v", err)
	}
	if _, err := portaltheme.Open(v, &captureSink{}); err == nil ||
		!strings.Contains(err.Error(), "register theme documents") {
		t.Fatalf("second open must fail on the documents bucket, got %v", err)
	}
}

func TestThemeOpenRejectsConflictingConfigBucket(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	defer func() { _ = v.Close() }()
	if _, err := vault.Register(v, "portal_theme_config", stringCodec{}); err != nil {
		t.Fatalf("pre-register: %v", err)
	}
	if _, err := portaltheme.Open(v, &captureSink{}); err == nil ||
		!strings.Contains(err.Error(), "register theme config") {
		t.Fatalf("open must fail on the config bucket, got %v", err)
	}
}

type stringCodec struct{}

func (stringCodec) Encode(value string) ([]byte, error) { return []byte(value), nil }
func (stringCodec) Decode(data []byte) (string, error)  { return string(data), nil }

// stubEngine is a programmable in-memory vault engine for the error branches a
// healthy engine cannot reach.
type stubEngine struct {
	mu         sync.Mutex
	buckets    map[vault.Name]map[string][]byte
	failUpdate bool
	failView   bool
}

func newStubEngine() *stubEngine {
	return &stubEngine{buckets: map[vault.Name]map[string][]byte{}}
}

var errStubEngine = errors.New("stub engine failure")

func (e *stubEngine) Update(_ context.Context, fn func(vault.EngineTxn) error) error {
	if e.failUpdate {
		return errStubEngine
	}

	return fn(stubTxn{engine: e, writable: true})
}

func (e *stubEngine) View(_ context.Context, fn func(vault.EngineTxn) error) error {
	if e.failView {
		return errStubEngine
	}

	return fn(stubTxn{engine: e})
}

func (e *stubEngine) Provision(name vault.Name) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.buckets[name]; !ok {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *stubEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *stubEngine) QuotaBytes() int64                        { return 0 }
func (e *stubEngine) Close() error                             { return nil }

func (e *stubEngine) corrupt(name vault.Name, key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.buckets[name][key] = []byte("corrupt-not-json")
}

type stubTxn struct {
	engine   *stubEngine
	writable bool
}

func (t stubTxn) Bucket(name vault.Name) vault.EngineBucket {
	return stubBucket{engine: t.engine, name: name}
}

func (t stubTxn) Writable() bool { return t.writable }

type stubBucket struct {
	engine *stubEngine
	name   vault.Name
}

func (b stubBucket) Get(key vault.Key) []byte {
	b.engine.mu.Lock()
	defer b.engine.mu.Unlock()

	return b.engine.buckets[b.name][string(key)]
}

func (b stubBucket) Put(key vault.Key, value []byte) error {
	b.engine.mu.Lock()
	defer b.engine.mu.Unlock()
	b.engine.buckets[b.name][string(key)] = value

	return nil
}

func (b stubBucket) Delete(key vault.Key) error {
	b.engine.mu.Lock()
	defer b.engine.mu.Unlock()
	delete(b.engine.buckets[b.name], string(key))

	return nil
}

func (b stubBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	b.engine.mu.Lock()
	defer b.engine.mu.Unlock()
	for key, value := range b.engine.buckets[b.name] {
		if !strings.HasPrefix(key, string(prefix)) {
			continue
		}
		if next, err := fn(vault.Key(key), value); err != nil || !next {
			return err
		}
	}

	return nil
}

func stubVault(t *testing.T, engine *stubEngine) *vault.Vault {
	t.Helper()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	return v
}

func TestThemeOpenSurfacesCorruptStoredState(t *testing.T) {
	for name, key := range map[string]struct{ bucket, key string }{
		"config":   {"portal_theme_config", "theme"},
		"document": {"portal_theme_docs", "search"},
	} {
		t.Run(name, func(t *testing.T) {
			engine := newStubEngine()
			v := stubVault(t, engine)
			theme, err := portaltheme.Open(v, &captureSink{})
			if err != nil {
				t.Fatalf("seed open: %v", err)
			}
			ctx := context.Background()
			if err := theme.SetEnabled(ctx, true); err != nil {
				t.Fatalf("enable: %v", err)
			}
			if _, err := theme.SaveDocument(ctx, portaltheme.PageSearch, "<p>x</p>"); err != nil {
				t.Fatalf("save: %v", err)
			}
			engine.corrupt(vault.Name(key.bucket), key.key)

			fresh := stubVault(t, newCopyEngine(engine))
			if _, err := portaltheme.Open(fresh, &captureSink{}); err == nil ||
				!strings.Contains(err.Error(), "load stored theme") {
				t.Fatalf("open over corrupt %s must fail, got %v", name, err)
			}
		})
	}
}

// newCopyEngine clones a seeded engine so a second vault can register the same
// buckets over the same bytes.
func newCopyEngine(from *stubEngine) *stubEngine {
	from.mu.Lock()
	defer from.mu.Unlock()
	engine := newStubEngine()
	for name, bucket := range from.buckets {
		copied := map[string][]byte{}
		for key, value := range bucket {
			copied[key] = append([]byte(nil), value...)
		}
		engine.buckets[name] = copied
	}

	return engine
}

func TestThemeWriteAndReadErrorsSurface(t *testing.T) {
	engine := newStubEngine()
	v := stubVault(t, engine)
	theme, err := portaltheme.Open(v, &captureSink{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if _, err := theme.SaveDocument(ctx, portaltheme.PageSearch, "<p>x</p>"); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	engine.failUpdate = true
	if err := theme.SetEnabled(ctx, true); err == nil ||
		!strings.Contains(err.Error(), "store theme toggle") {
		t.Errorf("SetEnabled must surface the vault error, got %v", err)
	}
	if _, err := theme.SaveDocument(ctx, portaltheme.PageSearch, "<p>y</p>"); err == nil ||
		!strings.Contains(err.Error(), "store theme document") {
		t.Errorf("SaveDocument must surface the vault error, got %v", err)
	}
	if _, err := theme.ResetDocument(ctx, portaltheme.PageSearch); err == nil ||
		!strings.Contains(err.Error(), "reset theme document") {
		t.Errorf("ResetDocument must surface the vault error, got %v", err)
	}

	engine.failView = true
	if _, _, err := theme.Document(ctx, portaltheme.PageSearch); err == nil ||
		!strings.Contains(err.Error(), "load theme document") {
		t.Errorf("Document must surface the vault error, got %v", err)
	}
}

func TestThemeDocumentSurfacesCorruptRead(t *testing.T) {
	engine := newStubEngine()
	v := stubVault(t, engine)
	theme, err := portaltheme.Open(v, &captureSink{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if _, err := theme.SaveDocument(ctx, portaltheme.PageSearch, "<p>x</p>"); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	engine.corrupt("portal_theme_docs", "search")
	if _, _, err := theme.Document(ctx, portaltheme.PageSearch); err == nil ||
		!strings.Contains(err.Error(), "load theme document") {
		t.Fatalf("Document must surface the corrupt read, got %v", err)
	}
}

// TestThemeResetSurfacesDeleteError drives ResetDocument's in-transaction delete
// error: a corrupt length counter makes the collection's Delete fail when it
// adjusts the count, a branch a plain memvault delete never reaches.
func TestThemeResetSurfacesDeleteError(t *testing.T) {
	engine := newStubEngine()
	v := stubVault(t, engine)
	theme, err := portaltheme.Open(v, &captureSink{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if _, err := theme.SaveDocument(ctx, portaltheme.PageResults, "<p>x</p>"); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	engine.corrupt("__lengths__", "portal_theme_docs")
	if _, err := theme.ResetDocument(ctx, portaltheme.PageResults); err == nil ||
		!strings.Contains(err.Error(), "reset theme document") {
		t.Fatalf("ResetDocument must surface the delete error, got %v", err)
	}
}

func TestThemeEventMessagesNameTheDocument(t *testing.T) {
	theme, sink := openTheme(t)
	ctx := context.Background()

	if _, err := theme.SaveDocument(ctx, portaltheme.PageResults, "<p>x</p>"); err != nil {
		t.Fatalf("save: %v", err)
	}
	want := fmt.Sprintf("portal theme document %q saved (8 bytes)", portaltheme.PageResults)
	if got := sink.messages(); len(got) != 1 || got[0] != want {
		t.Errorf("save event = %q, want %q", got, want)
	}
}
