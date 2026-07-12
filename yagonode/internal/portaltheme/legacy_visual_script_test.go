package portaltheme

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRepairLegacyVisualScriptRestoresDefaultOperators(t *testing.T) {
	clickCapture := `(function () {
  var results = document.querySelector("ul.results[data-t]");
  if (!results || !navigator.sendBeacon) return;
  results.addEventListener("click", function (event) {
    var link = event.target.closest("a[data-p][data-i]");
    if (!link) return;
    var body = new URLSearchParams();
    body.set("t", results.getAttribute("data-t"));
    body.set("i", link.getAttribute("data-i"));
    body.set("p", link.getAttribute("data-p"));
    navigator.sendBeacon("/searchclick", body);
  });
})();`
	extended := strings.Replace(
		defaultScripts,
		"\n</script>",
		"\n"+clickCapture+"\n</script>",
		1,
	)
	body := "<main>custom layout</main>" + strings.Replace(
		extended,
		defaultAutocompleteScript,
		legacyBrokenAutocompleteScript,
		1,
	) + "<footer>custom footer</footer>"
	want := "<main>custom layout</main>" + extended + "<footer>custom footer</footer>"
	if repaired := repairLegacyVisualScript(body); repaired != want {
		t.Fatalf("repaired script = %q, want %q", repaired, want)
	}
}

func TestRepairLegacyVisualScriptLeavesOperatorCodeAlone(t *testing.T) {
	body := `<pre>if (!input  !list) return;</pre><script>` +
		`var sample = "render((data && data[1])  []);"; var custom = left || right;</script>`
	if repaired := repairLegacyVisualScript(body); repaired != body {
		t.Fatalf("custom script changed: %q", repaired)
	}
}

func TestThemeLoadsLegacyVisualScriptAsRepaired(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	theme, err := Open(v, nopSink{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	broken := strings.Replace(
		defaultResultsBody,
		defaultAutocompleteScript,
		legacyBrokenAutocompleteScript,
		1,
	)
	if err := theme.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return theme.docs.Put(tx, vault.Key(PageResults), Document{Body: broken, ParseOK: true})
	}); err != nil {
		t.Fatalf("store legacy document: %v", err)
	}
	if err := theme.reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := theme.SetEnabled(t.Context(), true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	document, found, err := theme.Document(t.Context(), PageResults)
	if err != nil || !found || strings.Contains(document.Body, "input  !list") {
		t.Fatalf("document = %#v found=%v err=%v", document, found, err)
	}
	html, ok := theme.Render(t.Context(), PageResults, map[string]any{})
	if !ok || strings.Contains(html, "data[1])  []") {
		t.Fatalf("rendered legacy document = %q ok=%v", html, ok)
	}
}

func TestThemeSavesLegacyVisualScriptAsRepaired(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	theme, err := Open(v, nopSink{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	broken := strings.Replace(
		defaultSearchBody,
		defaultAutocompleteScript,
		legacyBrokenAutocompleteScript,
		1,
	)
	document, err := theme.SaveDocument(t.Context(), PageSearch, broken)
	if err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}
	if document.Body != defaultSearchBody {
		t.Fatalf("saved document did not repair the exact default script")
	}
}
