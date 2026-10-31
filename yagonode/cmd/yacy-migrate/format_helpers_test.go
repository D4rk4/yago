package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentimport"
)

func TestSolrPreservationMetadataAcceptsAndRejectsBoundaries(t *testing.T) {
	metadata := make(map[string]string)
	links := solrDocumentLinks{
		outbound: []string{"https://out.example.test/"}, outboundAvailable: 2,
		inbound:          []yagocrawlcontract.AnchorText{{URL: "https://in.example.test/"}},
		inboundAvailable: 3,
	}
	err := addSolrPreservationMetadata(metadata, solrDocumentURLs{
		legacyURLStatus: documentimport.LegacyURLStatusInvalidEscape, credentialsRedacted: true,
	}, solrDocumentContent{fullTitle: "complete title", fullHeadings: []solrFullHeading{{
		Field: "h1_txt", Index: 0, Text: "complete heading",
	}}}, links)
	if err != nil {
		t.Fatalf("add preserved metadata: %v", err)
	}
	for key, want := range map[string]string{
		documentimport.LegacyURLStatusMetadataKey: "invalid_percent_escape_in_url",
		"yacy.migration_credentials_redacted":     "true",
		"yacy.title_full":                         "complete title",
		"yacy.migration_title_truncated":          "true",
		"yacy.migration_headings_truncated":       "true",
	} {
		if metadata[key] != want {
			t.Errorf("preservation metadata %s = %q, want %q", key, metadata[key], want)
		}
	}
	var truncation migrationLinkTruncation
	if err := json.Unmarshal(
		[]byte(metadata[migrationLinkTruncationMetadataKey]),
		&truncation,
	); err != nil ||
		truncation.Outbound.Available != 2 || truncation.Outbound.Stored != 1 ||
		truncation.Inbound.Available != 3 ||
		truncation.Inbound.Stored != 1 {
		t.Fatalf(
			"link truncation metadata = %q, err=%v",
			metadata[migrationLinkTruncationMetadataKey],
			err,
		)
	}
	if err := addSolrPreservationMetadata(
		map[string]string{}, solrDocumentURLs{}, solrDocumentContent{}, solrDocumentLinks{},
	); err != nil {
		t.Fatalf("empty preservation metadata: %v", err)
	}
	if err := addFullTitleMetadata(
		map[string]string{}, strings.Repeat("x", documentimport.MaximumRecordMetadataBytes),
	); err == nil {
		t.Fatal("oversized full title was accepted")
	}
	if err := addSolrPreservationMetadata(
		map[string]string{
			"existing": strings.Repeat("x", documentimport.MaximumRecordMetadataBytes),
		},
		solrDocumentURLs{},
		solrDocumentContent{},
		solrDocumentLinks{},
	); err == nil {
		t.Fatal("metadata exceeding the aggregate limit was accepted")
	}
	if err := addSolrPreservationMetadata(
		map[string]string{},
		solrDocumentURLs{},
		solrDocumentContent{
			fullTitle: strings.Repeat("x", documentimport.MaximumRecordMetadataBytes),
		},
		solrDocumentLinks{},
	); err == nil {
		t.Fatal("oversized title failed to propagate from preservation metadata")
	}
}

func TestSolrURLMappingRejectsUnsupportedPrimaryAndCanonicalForms(t *testing.T) {
	for _, source := range []map[string]json.RawMessage{
		{"sku": mustJSONRaw(t, "https://example.test/%zz#fragment")},
		{"sku": mustJSONRaw(t, "https:///missing-host")},
		{
			"sku":         mustJSONRaw(t, "https://example.test/"),
			"canonical_s": mustJSONRaw(t, "https://example.test/%zz#fragment"),
		},
		{
			"sku":         mustJSONRaw(t, "https://example.test/"),
			"canonical_s": mustJSONRaw(t, "file://example.test/path"),
		},
	} {
		if _, err := mapSolrURLs(source); err == nil {
			t.Errorf("unsupported URL mapping input %v was accepted", source)
		}
	}
	if _, err := mapSolrURLs(map[string]json.RawMessage{
		"sku":         mustJSONRaw(t, "https://example.test/"),
		"canonical_s": mustJSONRaw(t, "not an absolute URL"),
	}); err == nil {
		t.Fatal("relative canonical URL was accepted")
	}
}

func TestMalformedCredentialRedactionRequiresSupportedAuthority(t *testing.T) {
	for _, raw := range []string{
		"relative/path",
		"file://user:secret@example.test/path%zz",
		"https://@example.test/path%zz",
		"https://user@/path%zz",
	} {
		if _, changed := redactMalformedURLUserinfo(raw); changed {
			t.Errorf("malformed URL %q was unexpectedly redacted", raw)
		}
	}
	if got, changed := redactMalformedURLUserinfo(
		"https://user:bad%zz@example.test/path%zz",
	); !changed ||
		got != "https://example.test/path%zz" {
		t.Fatalf("malformed credential redaction = %q, %t", got, changed)
	}
}

func TestSolrURLAndParseHelpersClassifyShapes(t *testing.T) {
	for _, fixture := range []struct {
		raw  string
		want bool
	}{
		{"http://example.test/", true},
		{"HTTPS://example.test/", true},
		{"ftp://example.test/file", false},
		{"not a URL", false},
	} {
		if got := isHTTPURL(fixture.raw); got != fixture.want {
			t.Errorf("isHTTPURL(%q) = %t, want %t", fixture.raw, got, fixture.want)
		}
	}
	for _, fixture := range []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: "other parser rejection"},
		{name: "generic", err: errors.New("private"), want: "other parser rejection"},
		{name: "missing wrapped error", err: &url.Error{URL: "private"}, want: "other parser rejection"},
		{
			name: "encoded host whitespace",
			err:  parseErrorForURL(t, "https://bad%20host.example.test/"),
			want: "percent-encoded whitespace in host",
		},
		{name: "other escape", err: parseErrorForURL(t, "https://example.test/%zz"), want: "invalid percent escape"},
		{name: "invalid host", err: parseErrorForURL(t, "https://bad host.example.test/"), want: "invalid host character"},
		{name: "unknown url error", err: &url.Error{Op: "parse", URL: "private", Err: errors.New("other")}, want: "other parser rejection"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if got := safeURLParseError(fixture.err); got != fixture.want {
				t.Fatalf("safeURLParseError() = %q, want %q", got, fixture.want)
			}
		})
	}
	for _, fixture := range []struct {
		raw  string
		want string
	}{
		{"\nhttps://example.test/", "control character in source URL"},
		{"https://example.test/#fragment", "fragment or backslash in source URL"},
		{"relative/path", "missing scheme-authority separator"},
		{"file://example.test/path", "unsupported source scheme"},
		{"https:///path", "empty source authority"},
		{"https://user:password@example.test/path", "userinfo-bearing source authority"},
		{"https://bad%zz.example.test/path", "malformed percent escape in source authority"},
		{"https://bad%2f.example.test/path", "percent escape rejected in source authority"},
		{"https://example.test/path%zz", "malformed percent escape after source authority"},
		{"https://example.test/path", "unclassified source URL shape (bytes=25; percent_signs=0; malformed_percent=false)"},
	} {
		if got := safeURLParseContext(fixture.raw); got != fixture.want {
			t.Errorf("safeURLParseContext(%q) = %q, want %q", fixture.raw, got, fixture.want)
		}
	}
	for _, fixture := range []struct {
		raw  string
		want bool
	}{
		{"", false}, {"%", true}, {"%xz", true}, {"%2f", false}, {"%2f%", true},
	} {
		if got := hasMalformedPercentEscape(fixture.raw); got != fixture.want {
			t.Errorf("hasMalformedPercentEscape(%q) = %t, want %t", fixture.raw, got, fixture.want)
		}
	}
	if isUTF8Boundary("éx", 1) || !isUTF8Boundary("éx", 0) || !isUTF8Boundary("éx", 2) {
		t.Fatal("UTF-8 boundary checks returned an incorrect result")
	}
	if !isUTF8Boundary("abc", 1) {
		t.Fatal("ASCII byte boundary was rejected")
	}
	if got := truncateUTF8("éx", 1); got != "" {
		t.Fatalf("truncated UTF-8 value = %q", got)
	}
	if got := truncateUTF8("short", 10); got != "short" {
		t.Fatalf("untruncated value = %q", got)
	}
}

func TestSolrRedirectMetadataRedactsRecursiveCredentials(t *testing.T) {
	metadata := map[string]string{}
	redirect := map[string]any{
		"primary": "https://user:secret@example.test/a",
		"nested":  []any{"https://next:secret@example.test/b"},
	}
	mapSolrRedirectMetadata(map[string]json.RawMessage{
		"httpstatus_redirect_s": mustJSONRaw(t, redirect),
	}, metadata)
	if metadata["yacy.redirect"] == "" ||
		!strings.Contains(metadata["yacy.redirect"], "https://example.test/a") ||
		!strings.Contains(metadata["yacy.redirect"], "https://example.test/b") ||
		metadata["yacy.migration_credentials_redacted"] != "true" {
		t.Fatalf("redirect metadata = %#v", metadata)
	}
	mapSolrRedirectMetadata(map[string]json.RawMessage{
		"httpstatus_redirect_s": json.RawMessage("not-json"),
	}, metadata)
	if _, exists := metadata["yacy.redirect"]; !exists {
		t.Fatal("invalid redirect replaced existing metadata")
	}
	if err := validateSolrMetadataBounds(map[string]string{
		"large": strings.Repeat("x", documentimport.MaximumRecordMetadataBytes),
	}); err == nil {
		t.Fatal("oversized Solr metadata was accepted")
	}
}

func TestRedactURLCredentialsHandlesNestedAndUnchangedValues(t *testing.T) {
	input := map[string]any{
		"credentialed": "https://user:secret@example.test/path",
		"unchanged":    "not a URL",
		"nested":       []any{"https://next:secret@example.test/path", 7, nil},
	}
	got, redacted := redactURLCredentials(input)
	if !redacted {
		t.Fatal("nested credentials were not detected")
	}
	want := map[string]any{
		"credentialed": "https://example.test/path",
		"unchanged":    "not a URL",
		"nested":       []any{"https://example.test/path", 7, nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("redacted value = %#v, want %#v", got, want)
	}
	for _, value := range []any{
		"https://example.test/path",
		"https://example.test:bad/path",
		"https:///path",
		7,
	} {
		if _, changed := redactURLCredentials(value); changed {
			t.Errorf("unchanged value %#v was marked redacted", value)
		}
	}
}

func TestSolrMetadataLinksAndPositionsCoverRemainingInputs(t *testing.T) {
	metadata, err := mapSolrMetadata(map[string]json.RawMessage{
		"httpstatus_redirect_s": json.RawMessage("not-json"),
		"id":                    mustJSONRaw(t, "https://user:secret@example.test/id"),
	})
	if err != nil || metadata["yacy.source_id"] != "https://example.test/id" {
		t.Fatalf("metadata = %#v, err=%v", metadata, err)
	}
	if got := solrStringPositions(json.RawMessage("null")); got != nil {
		t.Fatalf("null positions = %#v", got)
	}
	if got := solrStringPositions(mustJSONRaw(t, "raw-position")); !reflect.DeepEqual(
		got, []string{"raw-position"},
	) {
		t.Fatalf("scalar positions = %#v", got)
	}
	if got := solrStringPositions(json.RawMessage("[\"one\",null,\"three\"]")); !reflect.DeepEqual(
		got, []string{"one", "", "three"},
	) {
		t.Fatalf("sparse positions = %#v", got)
	}
	for _, fixture := range []struct {
		protocols string
		stubs     string
		wantErr   bool
	}{
		{protocols: "[]", stubs: "[]"},
		{protocols: "[\"-https\"]", stubs: "[\"example.test\"]", wantErr: true},
		{protocols: "[\"x-https\"]", stubs: "[\"example.test\"]", wantErr: true},
		{protocols: "[\"001-https\"]", stubs: "[\"example.test\"]", wantErr: true},
		{protocols: "[\"000-https\",\"000-ftp\"]", stubs: "[\"example.test\"]", wantErr: true},
		{protocols: "[]", stubs: "[\"example.test/a\"]"},
		{protocols: "[\"000-file\"]", stubs: "[\"example.test/a\"]"},
	} {
		_, _, linkErr := solrLinks(
			json.RawMessage(fixture.protocols), json.RawMessage(fixture.stubs), 1,
		)
		if (linkErr != nil) != fixture.wantErr {
			t.Errorf("solrLinks(%s, %s) error = %v", fixture.protocols, fixture.stubs, linkErr)
		}
	}
}

func parseErrorForURL(t *testing.T, raw string) error {
	t.Helper()
	_, err := url.Parse(raw)
	if err == nil {
		t.Fatalf("URL %q parsed without an error", raw)
	}
	return fmt.Errorf("parse fixture URL: %w", err)
}

func mustJSONRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
