package adminui

import (
	"net/http"
	"strings"
	"testing"
)

type fakeSuggest struct{}

func (fakeSuggest) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-suggestions+json")
	_, _ = w.Write([]byte(`["go",["golang tutorial"]]`))
}

func TestConsoleSearchRendersAutocompleteWhenSuggestWired(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Search:        fakeSearch{},
		SearchSuggest: fakeSuggest{},
	})
	got := do(t, console, "/admin/search")
	for _, want := range []string{
		`role="combobox"`, `id="ac-list"`, `role="listbox"`,
		`<script src="/admin/assets/autocomplete.js" defer></script>`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("search page missing %q", want)
		}
	}

	suggest := do(t, console, "/admin/search/suggest?q=go")
	if suggest.status != http.StatusOK ||
		!strings.Contains(suggest.body, "golang tutorial") {
		t.Fatalf("suggest route = %d %q", suggest.status, suggest.body)
	}
}

func TestConsoleSearchPlainInputWithoutSuggest(t *testing.T) {
	t.Parallel()

	console := New(Options{Search: fakeSearch{}})
	got := do(t, console, "/admin/search")
	if strings.Contains(got.body, `role="combobox"`) ||
		strings.Contains(got.body, "autocomplete.js") {
		t.Fatal("autocomplete rendered without a suggest handler")
	}
	if suggest := do(
		t,
		console,
		"/admin/search/suggest?q=go",
	); suggest.status != http.StatusNotFound {
		t.Fatalf("suggest route without handler = %d, want 404", suggest.status)
	}
}
