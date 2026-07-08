package portaltheme

import (
	"context"
	"strings"
	"testing"

	"github.com/mailgun/raymond/v2"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

type nopSink struct{}

func (nopSink) Record(events.Severity, events.Category, string, string) {}

// TestRenderRecoversFromPanicAndLogsOnce forces the panic branch raymond can
// reach through Exec: the public portal must fall back to the built-in render
// instead of crashing the request, and a persistently failing template must
// warn once per saved body rather than once per request.
func TestRenderRecoversFromPanicAndLogsOnce(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	theme, err := Open(v, nopSink{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if err := theme.SetEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := theme.SaveDocument(ctx, PageSearch, "<p>{{query}}</p>"); err != nil {
		t.Fatalf("save: %v", err)
	}

	original := execTemplate
	defer func() { execTemplate = original }()
	execTemplate = func(*raymond.Template, any) (string, error) {
		panic("boom")
	}

	for range 3 {
		if _, ok := theme.Render(context.Background(), PageSearch, map[string]any{}); ok {
			t.Fatal("a panicking render must fall back")
		}
	}
	theme.mu.RLock()
	failed := theme.failed[PageSearch]
	theme.mu.RUnlock()
	if !failed {
		t.Fatal("the failure flag must latch after a panicking render")
	}

	if _, err := theme.SaveDocument(ctx, PageSearch, "<p>fixed {{query}}</p>"); err != nil {
		t.Fatalf("save fix: %v", err)
	}
	theme.mu.RLock()
	failed = theme.failed[PageSearch]
	theme.mu.RUnlock()
	if failed {
		t.Fatal("saving a new body must reset the failure flag")
	}

	execTemplate = original
	html, ok := theme.Render(context.Background(), PageSearch, map[string]any{"query": "back"})
	if !ok || !strings.Contains(html, "fixed back") {
		t.Fatalf("a fixed template must render again: %q %v", html, ok)
	}
}
