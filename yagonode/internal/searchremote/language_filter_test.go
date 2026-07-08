package searchremote

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestRowLanguageReadsPlainProperty(t *testing.T) {
	t.Parallel()

	row := yagomodel.URIMetadataRow{Properties: map[string]string{"lang": " RU "}}
	if got := rowLanguage(row); got != "ru" {
		t.Fatalf("rowLanguage = %q, want ru", got)
	}
	if got := rowLanguage(yagomodel.URIMetadataRow{Properties: map[string]string{}}); got != "" {
		t.Fatalf("missing lang = %q, want empty", got)
	}
}

func TestLanguageFilteredDropsContradictingRows(t *testing.T) {
	t.Parallel()

	rows := []searchcore.Result{
		{URL: "a", Language: "ru"},
		{URL: "b", Language: "rus"},
		{URL: "c", Language: ""},
		{URL: "d", Language: "en"},
	}

	kept := languageFiltered(searchcore.Request{Language: "ru"}, rows)
	if len(kept) != 3 || kept[0].URL != "a" || kept[1].URL != "b" || kept[2].URL != "c" {
		t.Fatalf("kept = %+v, want a,b,c (unknown language stays)", kept)
	}

	all := languageFiltered(searchcore.Request{}, rows)
	if len(all) != len(rows) {
		t.Fatalf("no filter without a language:, got %d rows", len(all))
	}

	short := languageFiltered(searchcore.Request{Language: "EN"}, rows)
	if len(short) != 2 || short[0].URL != "c" || short[1].URL != "d" {
		t.Fatalf("prefix compare failed: %+v", short)
	}
}
