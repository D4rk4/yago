package publicportal

import "testing"

func TestThemeViewCarriesResultReasons(t *testing.T) {
	view := (portalData{Results: SearchResults{Results: []SearchResult{{
		Reasons: []string{"The query matched the title."},
	}}}}).themeView()
	resultsView, ok := view["results"].(map[string]any)
	if !ok {
		t.Fatalf("results view = %#v", view["results"])
	}
	results, ok := resultsView["results"].([]map[string]any)
	if !ok || len(results) != 1 {
		t.Fatalf("result rows = %#v", resultsView["results"])
	}
	reasons, ok := results[0]["reasons"].([]string)
	if !ok || len(reasons) != 1 || reasons[0] != "The query matched the title." {
		t.Fatalf("result reasons = %#v", results[0]["reasons"])
	}
}
