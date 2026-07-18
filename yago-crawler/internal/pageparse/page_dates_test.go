package pageparse

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func TestReadPageDatesUsesStructuredSourcesInPriorityOrder(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<html><head>
<script type="text/plain">{"datePublished":"2020-01-01"}</script>
<script type="application/ld+json">not-json</script>
<script type="application/ld+json">{"name":"without dates"}</script>
<script type="application/ld+json; charset=utf-8">[
  {"name":"first"},
  {"@graph":{"article":{"datePublished":"2024-03-02T10:11:12+02:00"}}}
]</script>
<time itemprop="dateModified" datetime="2024-03-03T10:11:12"></time>
<meta property="article:modified_time" content="2024-03-04">
</head></html>`))
	if err != nil {
		t.Fatal(err)
	}
	published, modified, confidence, source := readPageDates(root)
	if published != time.Date(2024, 3, 2, 8, 11, 12, 0, time.UTC) {
		t.Fatalf("published = %v", published)
	}
	if modified != time.Date(2024, 3, 3, 10, 11, 12, 0, time.UTC) {
		t.Fatalf("modified = %v", modified)
	}
	if confidence != 0.9 || source != "json-ld+itemprop" {
		t.Fatalf("confidence/source = %v/%q", confidence, source)
	}
}

func TestReadPageDatesCombinesItemPropertyAndMeta(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<html><head>
<meta name="datePublished" content="bad">
<meta name="datePublished" content="2023-01-02">
<meta property="og:modified_time" content="2023-01-03T04:05:06Z">
</head><body>
<meta itemprop="datePublished" content="2023-01-01">
<time itemprop="ignored dateModified" datetime="bad"></time>
</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	published, modified, confidence, source := readPageDates(root)
	if published != time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("published = %v", published)
	}
	if modified != time.Date(2023, 1, 3, 4, 5, 6, 0, time.UTC) {
		t.Fatalf("modified = %v", modified)
	}
	if confidence != 0.8 || source != "itemprop+meta" {
		t.Fatalf("confidence/source = %v/%q", confidence, source)
	}
}

func TestReadPageDatesReturnsUnknownWithoutEvidence(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<html><head><meta name="other"></head></html>`))
	if err != nil {
		t.Fatal(err)
	}
	published, modified, confidence, source := readPageDates(root)
	if !published.IsZero() || !modified.IsZero() || confidence != 0 || source != "" {
		t.Fatalf("dates = %v %v %v %q", published, modified, confidence, source)
	}
}

func TestJSONDateTraversalAndParsingEdges(t *testing.T) {
	if published, modified := datesFromJSONValue(
		"value",
	); !published.IsZero() ||
		!modified.IsZero() {
		t.Fatalf("scalar dates = %v %v", published, modified)
	}
	if published, _ := datesFromJSONValue(map[string]any{
		"datePublished": float64(1),
	}); !published.IsZero() {
		t.Fatalf("numeric date = %v", published)
	}
	if published, _ := datesFromJSONValue(map[string]any{
		"z": map[string]any{"datePublished": "2022-01-01"},
		"a": map[string]any{"datePublished": "2021-01-01"},
	}); published.Year() != 2021 {
		t.Fatalf("deterministic nested date = %v", published)
	}
	if got := dateFromJSONProperty(
		map[string]any{"other": "2020-01-01"},
		"datepublished",
	); !got.IsZero() {
		t.Fatalf("missing property = %v", got)
	}
	for _, value := range []string{"2020-01-02T03:04:05.123456789Z", "2020-01-02", "bad"} {
		got := parsePageDate(value)
		if (value == "bad") != got.IsZero() {
			t.Fatalf("parse %q = %v", value, got)
		}
	}
}

func TestReadIndividualDateSourcesWithoutValues(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<html><head>
<script type="application/ld+json">{"datePublished":1}</script>
<meta property="other" content="2020-01-01">
</head><body><span itemprop="other"></span></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := readJSONLDDates(root); got != (pageDateCandidate{}) {
		t.Fatalf("json candidate = %#v", got)
	}
	if got := readItemPropertyDates(root); got != (pageDateCandidate{}) {
		t.Fatalf("item candidate = %#v", got)
	}
	if got := readMetaDates(root); got != (pageDateCandidate{}) {
		t.Fatalf("meta candidate = %#v", got)
	}
}
