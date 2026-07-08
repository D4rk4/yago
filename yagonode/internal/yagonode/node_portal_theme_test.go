package yagonode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/portaltheme"
)

func TestThemeEventSinkAdaptsRecorder(t *testing.T) {
	recorder := events.NewRecorder(4)
	if sink, ok := themeEventSink(recorder).(*events.Recorder); !ok || sink != recorder {
		t.Fatal("a live recorder must pass through the sink adapter")
	}

	noop := themeEventSink(nil)
	noop.Record(events.SeverityInfo, events.CategoryConfig, "portal.theme", "no-op")
	if recorded := recorder.Recent(4); len(recorded) != 0 {
		t.Fatalf("noop sink must not reach the recorder: %v", recorded)
	}
}

func TestMountNodePublicSearchServesOperatorTheme(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	theme, err := portaltheme.Open(v, themeEventSink(nil))
	if err != nil {
		t.Fatalf("open theme: %v", err)
	}
	ctx := context.Background()
	if err := theme.SetEnabled(ctx, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := theme.SaveDocument(
		ctx,
		portaltheme.PageSearch,
		"<html><body><h1>operator theme {{brand}}</h1></body></html>",
	); err != nil {
		t.Fatalf("save: %v", err)
	}

	toggles := &runtimeToggles{}
	toggles.SetPortalEnabled(true)
	mux := http.NewServeMux()
	mountNodePublicSearch(mux, publicSearchAssembly{
		storage: nodeStorage{
			postings:     publicSearchPostingIndex{},
			urlDirectory: publicSearchURLDirectory{},
		},
		identity: nodeidentity.Identity{NetworkName: "freeworld"},
		dht:      defaultPublicSearchDHTConfig(),
		client:   http.DefaultClient,
		toggles:  toggles,
		theme:    theme,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "operator theme") {
		t.Fatalf("themed portal not served through the public mount: %s", rec.Body.String())
	}
}

func TestAssembleNodeSurfacesPortalThemeOpenError(t *testing.T) {
	engine := newCtrlEngine()
	engine.failProvision["portal_theme_docs"] = true
	v := ctrlVault(t, engine)

	_, err := assembleNodeSurfaces(assembleSurfacesInput{
		ctx:    context.Background(),
		config: testConfig(t),
		vault:  v,
	})
	if err == nil {
		t.Fatal("assembleNodeSurfaces should surface the portal theme open error")
	}
}
