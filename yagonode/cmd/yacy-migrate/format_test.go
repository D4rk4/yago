package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentimport"
)

func TestSafeURLParseErrorDoesNotExposeInputURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "escape", raw: "https://private.example.test/%zz", want: "invalid percent escape"},
		{name: "host", raw: "https://private host.example.test/", want: "invalid host character"},
		{name: "port", raw: "https://private.example.test:abc/", want: "invalid port"},
		{name: "control", raw: "https://private.example.test/\n", want: "control character"},
		{name: "bracket", raw: "https://[private.example.test/", want: "malformed bracketed host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := url.Parse(tc.raw)
			if err == nil {
				t.Fatal("malformed URL unexpectedly parsed")
			}
			got := safeURLParseError(err)
			if got != tc.want {
				t.Fatalf("safeURLParseError() = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "private.example.test") ||
				strings.Contains(got, "private host") {
				t.Fatalf("safe parse error exposed source URL data: %q", got)
			}
		})
	}
}

func TestSafeURLParseContextReportsShapeWithoutURLValues(t *testing.T) {
	for _, fixture := range []struct {
		rawURL string
		want   string
	}{
		{
			rawURL: "https://private-user:private-password@host.example.test/%zz",
			want:   "userinfo-bearing source authority",
		},
		{
			rawURL: "https://bad%zzhost.example.test:443/path",
			want:   "malformed percent escape in source authority",
		},
		{
			rawURL: "https://bad%2fhost.example.test/path",
			want:   "percent escape rejected in source authority",
		},
		{
			rawURL: "https://host.example.test/path%zz",
			want:   "malformed percent escape after source authority",
		},
	} {
		got := safeURLParseContext(fixture.rawURL)
		if got != fixture.want {
			t.Fatalf("safeURLParseContext() = %q, want %q", got, fixture.want)
		}
		if strings.Contains(got, "private-user") || strings.Contains(got, "private-password") ||
			strings.Contains(got, "host.example.test") {
			t.Fatalf("safe URL context exposed input values: %q", got)
		}
	}
}

func TestMapSolrDocumentRedactsURLCredentialsAndDisablesRecrawl(t *testing.T) {
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"id":  json.RawMessage(`"https://iduser:idsecret@id.example.test/item"`),
		"sku": json.RawMessage(`"https://pageuser:pagesecret@page.example.test/item"`),
		"canonical_s": json.RawMessage(
			`"https://canonicaluser:canonicalsecret@canonical.example.test/item"`,
		),
		"httpstatus_redirect_s": json.RawMessage(
			`"https://redirectuser:redirectsecret@redirect.example.test/item"`,
		),
		"fresh_date_dt": json.RawMessage(`"2026-10-03T10:00:00Z"`),
	}, "fixture-source")
	if err != nil {
		t.Fatalf("map credential-bearing source row: %v", err)
	}
	if record.Document.NormalizedURL != "https://page.example.test/item" ||
		record.Document.CanonicalURL != "https://canonical.example.test/item" {
		t.Fatalf("credential-free URL identities not preserved: %#v", record.Document)
	}
	if record.NextVisitAt != nil {
		t.Fatal("credential-redacted URL was scheduled for unauthenticated recrawl")
	}
	if record.Document.Metadata["yacy.migration_credentials_redacted"] != "true" ||
		record.Document.Metadata["yacy.redirect"] != `"https://redirect.example.test/item"` ||
		record.Document.Metadata["yacy.source_id"] != "https://id.example.test/item" {
		t.Fatalf("credential redaction marker or metadata missing: %#v", record.Document.Metadata)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"pagesecret", "canonicalsecret", "redirectsecret", "idsecret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("migration record contains URL credential material %q", secret)
		}
	}
}

func TestMapSolrDocumentRedactsCredentialsFromMalformedLegacyHost(t *testing.T) {
	const rawURL = "https://fixture-user:fixture-secret@legacy host.example.test/page"
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":           json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"canonical_s":   json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"fresh_date_dt": json.RawMessage(`"2026-10-03T10:00:00Z"`),
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map credential-bearing malformed URL: %v", err)
	}
	const redactedURL = "https://legacy host.example.test/page"
	if record.Document.NormalizedURL != redactedURL || record.Document.CanonicalURL != redactedURL {
		t.Fatal("malformed URL credentials were not removed from the document identity")
	}
	if record.NextVisitAt != nil ||
		record.Document.Metadata["yacy.migration_credentials_redacted"] != "true" {
		t.Fatal("malformed credential-bearing URL was scheduled or lacked a redaction marker")
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "fixture-secret") ||
		strings.Contains(string(encoded), "fixture-user") {
		t.Fatal("malformed URL credentials were retained in the import record")
	}
}

func TestMapSolrDocumentPreservesFieldsAndStableHashes(t *testing.T) {
	source := map[string]json.RawMessage{
		"id":  json.RawMessage(`"yacy-id-1"`),
		"sku": json.RawMessage(`"https://library.example.test/item/7"`),
		"canonical_s": json.RawMessage(
			`"https://library.example.test/item/7?canonical=1"`,
		),
		"title":                      json.RawMessage(`"Example title"`),
		"text_t":                     json.RawMessage(`"sanitized archival fixture"`),
		"h1_txt":                     json.RawMessage(`["Main heading"]`),
		"language_s":                 json.RawMessage(`"en"`),
		"content_type":               json.RawMessage(`["text/html"]`),
		"httpstatus_i":               json.RawMessage(`200`),
		"load_date_dt":               json.RawMessage(`"2026-10-01T10:00:00Z"`),
		"last_modified":              json.RawMessage(`"2026-09-30T10:00:00Z"`),
		"collection_sxt":             json.RawMessage(`["canada-reference"]`),
		"focused_policy_sxt":         json.RawMessage(`["policy-a"]`),
		"outboundlinks_protocol_sxt": json.RawMessage(`["000-https"]`),
		"outboundlinks_urlstub_sxt":  json.RawMessage(`["archive.example.test/related"]`),
		"inboundlinks_protocol_sxt":  json.RawMessage(`["000-https"]`),
		"inboundlinks_urlstub_sxt":   json.RawMessage(`["catalog.example.test/ref"]`),
		"exact_signature_l":          json.RawMessage(`734`),
		"fresh_date_dt":              json.RawMessage(`"2026-10-03T10:00:00Z"`),
		"httpstatus_redirect_s":      json.RawMessage(`"https://library.example.test/new"`),
	}
	first, err := mapSolrDocument(source, "fixture-export")
	if err != nil {
		t.Fatalf("mapSolrDocument: %v", err)
	}
	second, err := mapSolrDocument(source, "fixture-export")
	if err != nil {
		t.Fatalf("repeat mapSolrDocument: %v", err)
	}
	if first.ID != second.ID || first.Document.ContentHash != second.Document.ContentHash {
		t.Fatal("same source row did not retain a stable ID and content hash")
	}
	wantNextVisit := time.Date(2026, time.October, 3, 10, 0, 0, 0, time.UTC)
	if first.NextVisitAt == nil || !first.NextVisitAt.Equal(wantNextVisit) {
		t.Fatalf("next visit = %v, want %v", first.NextVisitAt, wantNextVisit)
	}
	doc := first.Document
	if doc.NormalizedURL != "https://library.example.test/item/7" ||
		doc.CanonicalURL != "https://library.example.test/item/7?canonical=1" ||
		doc.Title != "Example title" || doc.Language != "en" || doc.ContentType != "text/html" ||
		doc.FetchStatus != "200" ||
		len(doc.Headings) != 1 || len(doc.Outlinks) != 1 || len(doc.Inlinks) != 1 {
		t.Fatalf("mapped document lost fields: %+v", doc)
	}
	if doc.Metadata["yacy.collections"] != `["canada-reference"]` {
		t.Fatalf("collection metadata missing: %q", doc.Metadata["yacy.collections"])
	}
	if doc.Metadata["yacy.redirect"] != `"https://library.example.test/new"` ||
		doc.Metadata["yacy.exact_signature"] != "734" {
		t.Fatalf("YaCy metadata gaps: %#v", doc.Metadata)
	}
}

func TestMapSolrDocumentPreservesLongDescriptionMetadata(t *testing.T) {
	description := strings.Repeat("x", (4<<20)+(256<<10))
	encoded, err := json.Marshal(description)
	if err != nil {
		t.Fatal(err)
	}
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":             json.RawMessage(`"https://library.example.test/item/long-description"`),
		"description_txt": encoded,
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map long source description: %v", err)
	}
	if got := record.Document.Metadata["yacy.description"]; got != string(encoded) {
		t.Fatalf("long source description was not preserved (got %d bytes)", len(got))
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate imported source description: %v", err)
	}
}

func TestMapSolrDocumentPreservesOversizedTitleInMetadata(t *testing.T) {
	fullTitle := strings.Repeat("é", yagocrawlcontract.MaximumDocumentTitleBytes/2+10)
	encoded, err := json.Marshal(fullTitle)
	if err != nil {
		t.Fatal(err)
	}
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":   json.RawMessage(`"https://library.example.test/long-title"`),
		"title": encoded,
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map long YaCy title: %v", err)
	}
	if len(record.Document.Title) > yagocrawlcontract.MaximumDocumentTitleBytes ||
		!strings.HasPrefix(fullTitle, record.Document.Title) {
		t.Fatalf("indexed title is not a bounded prefix: %d bytes", len(record.Document.Title))
	}
	if record.Document.Metadata["yacy.title_full"] != fullTitle ||
		record.Document.Metadata["yacy.migration_title_truncated"] != "true" {
		t.Fatal("full source title or truncation marker was not preserved")
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate long-title import record: %v", err)
	}
}

func TestMapSolrDocumentPreservesOversizedHeadingInMetadata(t *testing.T) {
	fullHeading := strings.Repeat("é", documentimport.MaximumRecordHeadingBytes/2+4)
	encoded, err := json.Marshal([]string{fullHeading})
	if err != nil {
		t.Fatal(err)
	}
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":    json.RawMessage(`"https://library.example.test/long-heading"`),
		"h2_txt": encoded,
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map long YaCy heading: %v", err)
	}
	if len(record.Document.Headings) != 1 ||
		len(record.Document.Headings[0]) > documentimport.MaximumRecordHeadingBytes ||
		!strings.HasPrefix(fullHeading, record.Document.Headings[0]) {
		t.Fatalf("indexed heading is not a bounded prefix: %#v", record.Document.Headings)
	}
	var preserved []solrFullHeading
	if err := json.Unmarshal(
		[]byte(record.Document.Metadata["yacy.headings_full"]),
		&preserved,
	); err != nil {
		t.Fatalf("decode preserved headings: %v", err)
	}
	if len(preserved) != 1 || preserved[0].Field != "h2_txt" || preserved[0].Index != 0 ||
		preserved[0].Text != fullHeading ||
		record.Document.Metadata["yacy.migration_headings_truncated"] != "true" {
		t.Fatal("full source heading, source position, or truncation marker was not preserved")
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate long-heading import record: %v", err)
	}
}

func TestMapSolrDocumentPreservesHeadingsBeyondTypedLimit(t *testing.T) {
	values := make([]string, documentimport.MaximumRecordHeadings+1)
	for index := range values {
		values[index] = "heading"
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":    json.RawMessage(`"https://library.example.test/many-headings"`),
		"h1_txt": encoded,
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map YaCy document with more than the typed heading limit: %v", err)
	}
	if len(record.Document.Headings) != documentimport.MaximumRecordHeadings {
		t.Fatalf("indexed heading count = %d, want %d", len(record.Document.Headings),
			documentimport.MaximumRecordHeadings)
	}
	var preserved []solrFullHeading
	if err := json.Unmarshal(
		[]byte(record.Document.Metadata["yacy.headings_full"]),
		&preserved,
	); err != nil {
		t.Fatalf("decode preserved headings: %v", err)
	}
	if len(preserved) != 1 || preserved[0].Field != "h1_txt" ||
		preserved[0].Index != documentimport.MaximumRecordHeadings ||
		preserved[0].Text != "heading" ||
		record.Document.Metadata["yacy.migration_headings_truncated"] != "true" {
		t.Fatal("heading beyond typed limit lost its source field, position, or full value")
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate overflow-heading import record: %v", err)
	}
}

func TestMapSolrDocumentPreservesMalformedYaCyURLWithoutRecrawl(t *testing.T) {
	rawURL := "https://bad%20host.example.test/document"
	row := map[string]json.RawMessage{
		"sku":            json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"canonical_s":    json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"title":          json.RawMessage(`"Legacy YaCy identity"`),
		"text_t":         json.RawMessage(`"preserved fixture content"`),
		"fresh_date_dt":  json.RawMessage(`"2026-10-03T10:00:00Z"`),
		"collection_sxt": json.RawMessage(`["canada-reference"]`),
	}
	record, err := mapSolrDocument(row, "fixture-export")
	if err != nil {
		t.Fatalf("map legacy YaCy URL identity: %v", err)
	}
	if record.Document.NormalizedURL != rawURL || record.Document.CanonicalURL != rawURL {
		t.Fatal("migration changed the source normalized or canonical URL")
	}
	if record.NextVisitAt != nil {
		t.Fatal("malformed legacy URL was incorrectly scheduled for recrawl")
	}
	if record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey] !=
		documentimport.LegacyURLStatusEncodedHostWhitespace {
		t.Fatal("legacy URL status metadata is missing")
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate preserved legacy record: %v", err)
	}
}

func TestMapSolrDocumentPreservesControlCharacterURLWithoutRecrawl(t *testing.T) {
	rawURL := "https://archive.example.test/path/\nrecord"
	encodedURL, err := json.Marshal(rawURL)
	if err != nil {
		t.Fatalf("encode fixture URL: %v", err)
	}
	row := map[string]json.RawMessage{
		"sku":            encodedURL,
		"canonical_s":    encodedURL,
		"title":          json.RawMessage(`"Control-character legacy identity"`),
		"text_t":         json.RawMessage(`"preserved fixture content"`),
		"fresh_date_dt":  json.RawMessage(`"2026-10-03T10:00:00Z"`),
		"collection_sxt": json.RawMessage(`["canada-reference"]`),
	}
	record, err := mapSolrDocument(row, "fixture-export")
	if err != nil {
		t.Fatalf("map control-character legacy URL: %v", err)
	}
	if record.Document.NormalizedURL != rawURL || record.Document.CanonicalURL != rawURL {
		t.Fatal("migration changed the source normalized or canonical URL")
	}
	if record.NextVisitAt != nil {
		t.Fatal("control-character legacy URL was scheduled for recrawl")
	}
	if got := record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey]; got != documentimport.LegacyURLStatusControlCharacter {
		t.Fatalf("legacy URL status = %q, want control-character marker", got)
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate preserved legacy record: %v", err)
	}
}

func TestMapSolrDocumentPreservesMalformedLegacyNonHTTPProtocolWithoutRecrawl(t *testing.T) {
	for _, rawURL := range []string{
		"ftp://legacy host.example.test/archive.zip",
		"smb://legacy host.example.test/share/document.pdf",
	} {
		t.Run(strings.SplitN(rawURL, ":", 2)[0], func(t *testing.T) {
			record, err := mapSolrDocument(map[string]json.RawMessage{
				"sku":           json.RawMessage(fmt.Sprintf("%q", rawURL)),
				"canonical_s":   json.RawMessage(fmt.Sprintf("%q", rawURL)),
				"fresh_date_dt": json.RawMessage(`"2026-10-03T10:00:00Z"`),
			}, "fixture-export")
			if err != nil {
				t.Fatalf("map legacy protocol URL: %v", err)
			}
			if record.Document.NormalizedURL != rawURL || record.Document.CanonicalURL != rawURL {
				t.Fatal("migration changed the source URL identity")
			}
			if record.NextVisitAt != nil {
				t.Fatal("non-HTTP legacy URL was scheduled for recrawl")
			}
			if record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey] !=
				documentimport.LegacyURLStatusInvalidHostCharacter {
				t.Fatal("legacy URL status metadata is missing")
			}
			if err := validateImportRecord(record); err != nil {
				t.Fatalf("validate legacy protocol record: %v", err)
			}
		})
	}
}

func TestMapSolrDocumentPreservesOtherInvalidHostWithoutRecrawl(t *testing.T) {
	for _, rawURL := range []string{
		"https://legacy host.example.test/document",
		`https://legacy\host.example.test/document`,
		"https://legacy host.example.test/document#stored-fragment",
	} {
		t.Run(rawURL, func(t *testing.T) {
			assertInvalidHostIdentityPreserved(t, rawURL)
		})
	}
}

func assertInvalidHostIdentityPreserved(t *testing.T, rawURL string) {
	t.Helper()
	encoded, err := json.Marshal(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]json.RawMessage{
		"sku": encoded, "canonical_s": encoded,
		"fresh_date_dt": json.RawMessage(`"2026-10-03T10:00:00Z"`),
	}
	record, err := mapSolrDocument(row, "fixture-export")
	if err != nil {
		t.Fatalf("map invalid-host YaCy identity: %v", err)
	}
	if record.Document.NormalizedURL != rawURL || record.Document.CanonicalURL != rawURL {
		t.Fatal("migration changed the invalid-host source identity")
	}
	if record.NextVisitAt != nil {
		t.Fatal("invalid-host legacy identity was scheduled for recrawl")
	}
	if record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey] !=
		documentimport.LegacyURLStatusInvalidHostCharacter {
		t.Fatalf(
			"invalid-host marker = %q",
			record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey],
		)
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate invalid-host record: %v", err)
	}
}

func TestMapSolrDocumentPreservesMalformedPercentEscapeInLegacyHost(t *testing.T) {
	rawURL := "https://archive%zz.example.test/document?format=html%20"
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":           json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"canonical_s":   json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"fresh_date_dt": json.RawMessage(`"2026-10-03T10:00:00Z"`),
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map malformed-percent legacy URL identity: %v", err)
	}
	if record.Document.NormalizedURL != rawURL || record.Document.CanonicalURL != rawURL {
		t.Fatal("migration changed the malformed-percent source URL identity")
	}
	if record.NextVisitAt != nil {
		t.Fatal("malformed legacy URL was incorrectly scheduled for recrawl")
	}
	if got := record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey]; got != documentimport.LegacyURLStatusInvalidHostEscape {
		t.Fatalf("legacy URL status = %q, want malformed percent escape in host", got)
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate preserved malformed-percent record: %v", err)
	}
}

func TestMapSolrDocumentPreservesPercentEncodedLegacyHostWithoutRecrawl(t *testing.T) {
	rawURL := "https://bad%2fhost.example.test/document"
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":           json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"canonical_s":   json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"fresh_date_dt": json.RawMessage(`"2026-10-03T10:00:00Z"`),
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map percent-encoded legacy host: %v", err)
	}
	if record.Document.NormalizedURL != rawURL || record.Document.CanonicalURL != rawURL {
		t.Fatal("migration changed the percent-encoded source URL identity")
	}
	if record.NextVisitAt != nil ||
		record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey] !=
			documentimport.LegacyURLStatusInvalidHostEscape {
		t.Fatal("opaque percent-encoded host was scheduled or lacks a legacy status")
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate opaque percent-encoded host: %v", err)
	}
}

func TestMapSolrDocumentPreservesRejectedAuthorityWithoutRecrawl(t *testing.T) {
	rawURL := "https://bad%20host.example.test:bad/path"
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":           json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"canonical_s":   json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"fresh_date_dt": json.RawMessage(`"2026-10-03T10:00:00Z"`),
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map rejected legacy authority: %v", err)
	}
	if record.Document.NormalizedURL != rawURL || record.Document.CanonicalURL != rawURL {
		t.Fatal("migration rewrote the opaque URL identity")
	}
	if record.NextVisitAt != nil ||
		record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey] !=
			documentimport.LegacyURLStatusInvalidAuthority {
		t.Fatal("opaque authority was scheduled or lacks its legacy status")
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate rejected authority record: %v", err)
	}
}

func TestMapSolrDocumentPreservesMalformedPercentEscapeInPathWithoutRecrawl(t *testing.T) {
	rawURL := "https://archive.example.test/document%zz?format=html"
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":           json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"canonical_s":   json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"fresh_date_dt": json.RawMessage(`"2026-10-03T10:00:00Z"`),
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map malformed-percent legacy URL identity: %v", err)
	}
	if record.Document.NormalizedURL != rawURL || record.Document.CanonicalURL != rawURL {
		t.Fatal("migration changed the malformed-percent source URL identity")
	}
	if record.NextVisitAt != nil {
		t.Fatal("malformed legacy URL was incorrectly scheduled for recrawl")
	}
	if got := record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey]; got != documentimport.LegacyURLStatusInvalidEscape {
		t.Fatalf("legacy URL status = %q, want malformed percent escape", got)
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate preserved malformed-percent record: %v", err)
	}
}

func TestMapSolrDocumentPreservesMalformedPercentEscapeInQueryWithoutRecrawl(t *testing.T) {
	rawURL := "https://archive.example.test/document?view=%"
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":           json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"canonical_s":   json.RawMessage(fmt.Sprintf("%q", rawURL)),
		"fresh_date_dt": json.RawMessage(`"2026-10-03T10:00:00Z"`),
	}, "fixture-export")
	if err != nil {
		t.Fatalf("map malformed-query legacy URL identity: %v", err)
	}
	if record.Document.NormalizedURL != rawURL || record.Document.CanonicalURL != rawURL {
		t.Fatal("migration changed the malformed-query source URL identity")
	}
	if record.NextVisitAt != nil {
		t.Fatal("malformed-query URL was incorrectly scheduled for recrawl")
	}
	if got := record.Document.Metadata[documentimport.LegacyURLStatusMetadataKey]; got != documentimport.LegacyURLStatusInvalidEscape {
		t.Fatalf("legacy URL status = %q, want malformed percent escape", got)
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate preserved malformed-query record: %v", err)
	}
}

func TestMapSolrDocumentPreservesFTPAndSMBWithoutHTTPRecrawl(t *testing.T) {
	for _, rawURL := range []string{
		"ftp://archive.example.test/pub/item.pdf",
		"smb://archive.example.test/share/item.pdf",
	} {
		protocol, _, _ := strings.Cut(rawURL, ":")
		t.Run(protocol, func(t *testing.T) {
			freshDate := json.RawMessage(`"2026-10-03T10:00:00Z"`)
			protocol, stub, separator := strings.Cut(rawURL, "://")
			stub = strings.TrimPrefix(stub, "archive.example.test/")
			record, err := mapSolrDocument(map[string]json.RawMessage{
				"sku":           json.RawMessage(fmt.Sprintf("%q", rawURL)),
				"fresh_date_dt": freshDate,
				"outboundlinks_protocol_sxt": json.RawMessage(fmt.Sprintf(
					`[%q]`, "000-"+protocol,
				)),
				"outboundlinks_urlstub_sxt": json.RawMessage(fmt.Sprintf(
					`[%q]`, "linked.example.test/"+stub,
				)),
			}, "fixture-export")
			if err != nil {
				t.Fatalf("map non-HTTP source row: %v", err)
			}
			if record.Document.NormalizedURL != rawURL || record.NextVisitAt != nil {
				t.Fatalf("non-HTTP identity or schedule changed: %+v", record)
			}
			if record.Document.Metadata["yacy.fresh_until"] != string(freshDate) {
				t.Fatal("source freshness metadata was not preserved")
			}
			if !separator || len(record.Document.Outlinks) != 1 ||
				record.Document.Outlinks[0] != protocol+"://linked.example.test/"+stub {
				t.Fatalf("non-HTTP link was not preserved: %#v", record.Document.Outlinks)
			}
			if err := validateImportRecord(record); err != nil {
				t.Fatalf("validate non-HTTP source row: %v", err)
			}
		})
	}
}

func TestMapSolrDocumentRejectsIncompleteAndAmbiguousRows(t *testing.T) {
	if _, err := mapSolrDocument(map[string]json.RawMessage{}, "fixture"); err == nil {
		t.Fatal("row without sku was accepted")
	}
	if _, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":                        json.RawMessage(`"https://example.test/"`),
		"outboundlinks_protocol_sxt": json.RawMessage(`["invalid"]`),
	}, "fixture"); err == nil {
		t.Fatal("malformed indexed link protocol was accepted")
	}
}

func TestMapSolrDocumentDerivesIDWhenYaCyIDIsAbsent(t *testing.T) {
	row := map[string]json.RawMessage{
		"sku":    json.RawMessage(`"https://fixture.example.test/page"`),
		"text_t": json.RawMessage(`"stable content"`),
	}
	first, err := mapSolrDocument(row, "fixture-source")
	if err != nil {
		t.Fatalf("map missing-ID row: %v", err)
	}
	second, err := mapSolrDocument(row, "fixture-source")
	if err != nil {
		t.Fatalf("map repeated missing-ID row: %v", err)
	}
	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("fallback IDs are not stable: %q and %q", first.ID, second.ID)
	}
	if first.NextVisitAt != nil {
		t.Fatalf("missing YaCy freshness date produced a next visit: %v", first.NextVisitAt)
	}
}

func TestMapSolrDocumentEnforcesRollbackSafeURLLimit(t *testing.T) {
	prefix := "https://archive.example.test/"
	atLimit := prefix + strings.Repeat("a", documentimport.MaximumMigrationURLBytes-len(prefix))
	record, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":         json.RawMessage(fmt.Sprintf("%q", atLimit)),
		"canonical_s": json.RawMessage(fmt.Sprintf("%q", atLimit)),
	}, "fixture-source")
	if err != nil {
		t.Fatalf("map URL at rollback-safe limit: %v", err)
	}
	if record.Document.NormalizedURL != atLimit || record.Document.CanonicalURL != atLimit {
		t.Fatal("URL at the rollback-safe limit was changed")
	}
	overLimit := atLimit + "a"
	if _, err := mapSolrDocument(map[string]json.RawMessage{
		"sku":         json.RawMessage(fmt.Sprintf("%q", overLimit)),
		"canonical_s": json.RawMessage(fmt.Sprintf("%q", overLimit)),
	}, "fixture-source"); err == nil {
		t.Fatal("URL beyond the rollback-safe limit was accepted")
	}
}

func TestMapSolrDocumentPreservesYaCyLargeHeadingList(t *testing.T) {
	headings := make([]string, 1331)
	for index := range headings {
		headings[index] = fmt.Sprintf("heading-%03d", index)
	}
	encoded, err := json.Marshal(headings)
	if err != nil {
		t.Fatalf("encode fixture headings: %v", err)
	}
	row := map[string]json.RawMessage{
		"sku":    json.RawMessage(`"https://fixture.example.test/long-page"`),
		"h1_txt": encoded,
	}
	record, err := mapSolrDocument(row, "fixture-source")
	if err != nil {
		t.Fatalf("map YaCy document with %d headings: %v", len(headings), err)
	}
	if len(record.Document.Headings) != len(headings) ||
		record.Document.Headings[len(headings)-1] != headings[len(headings)-1] {
		t.Fatalf(
			"mapped heading count/value = %d/%q",
			len(record.Document.Headings),
			record.Document.Headings[len(record.Document.Headings)-1],
		)
	}
}

func TestMapSolrDocumentPreservesLargeYaCyOutlinkList(t *testing.T) {
	const linkCount = 73_275
	protocols := make([]string, linkCount)
	stubs := make([]string, linkCount)
	fillSolrLinkArrays(protocols, stubs)
	row := map[string]json.RawMessage{
		"sku": json.RawMessage(`"https://fixture.example.test/high-link-count"`),
	}
	row["outboundlinks_protocol_sxt"], _ = json.Marshal(protocols)
	row["outboundlinks_urlstub_sxt"], _ = json.Marshal(stubs)

	record, err := mapSolrDocument(row, "fixture-source")
	if err != nil {
		t.Fatalf("map YaCy document with %d outlinks: %v", linkCount, err)
	}
	if len(record.Document.Outlinks) != linkCount {
		t.Fatalf("mapped outlinks = %d, want %d", len(record.Document.Outlinks), linkCount)
	}
	if len(record.Document.OutboundAnchors) != 0 || record.Document.OutboundAnchorEvidenceKnown {
		t.Fatal("URL-only YaCy links were mislabeled as anchor-text evidence")
	}
	if _, marked := record.Document.Metadata[migrationLinkTruncationMetadataKey]; marked {
		t.Fatal("complete in-bound link list was incorrectly marked truncated")
	}
}

func TestMapSolrDocumentBoundsOversizedLinkListsAndRecordsCounts(t *testing.T) {
	const extraOutlinks = 7
	const extraInlinks = 5
	outlinkCount := documentimport.MaximumRecordOutlinks + extraOutlinks
	outboundProtocols := make([]string, outlinkCount)
	outboundStubs := make([]string, outlinkCount)
	fillSolrLinkArrays(outboundProtocols, outboundStubs)
	inlinkCount := documentimport.MaximumRecordInlinks + extraInlinks
	inboundStubs := make([]string, inlinkCount)
	for index := range inboundStubs {
		inboundStubs[index] = fmt.Sprintf("source.example.test/%d", index)
	}
	row := map[string]json.RawMessage{
		"sku": json.RawMessage(`"https://fixture.example.test/high-link-count"`),
	}
	row["outboundlinks_protocol_sxt"], _ = json.Marshal(outboundProtocols)
	row["outboundlinks_urlstub_sxt"], _ = json.Marshal(outboundStubs)
	row["inboundlinks_urlstub_sxt"], _ = json.Marshal(inboundStubs)

	record, err := mapSolrDocument(row, "fixture-source")
	if err != nil {
		t.Fatalf("map oversized link lists: %v", err)
	}
	if len(record.Document.Outlinks) != documentimport.MaximumRecordOutlinks ||
		len(record.Document.Inlinks) != documentimport.MaximumRecordInlinks {
		t.Fatalf(
			"stored link counts = %d/%d",
			len(record.Document.Outlinks),
			len(record.Document.Inlinks),
		)
	}
	var truncation migrationLinkTruncation
	if err := json.Unmarshal(
		[]byte(record.Document.Metadata[migrationLinkTruncationMetadataKey]), &truncation,
	); err != nil {
		t.Fatalf("decode link truncation metadata: %v", err)
	}
	if truncation.Outbound.Available != outlinkCount ||
		truncation.Outbound.Stored != documentimport.MaximumRecordOutlinks ||
		truncation.Inbound.Available != inlinkCount ||
		truncation.Inbound.Stored != documentimport.MaximumRecordInlinks {
		t.Fatalf("link truncation details = %+v", truncation)
	}
	if err := validateImportRecord(record); err != nil {
		t.Fatalf("validate bounded link-list record: %v", err)
	}
}

func TestMapSolrDocumentRejectsInvalidURLsAndOversizedContent(t *testing.T) {
	assertSolrDocumentsRejected(t, []solrMappingRejection{
		{name: "invalid source URL", change: func(row map[string]json.RawMessage) {
			row["sku"] = json.RawMessage(`"file:///tmp/private"`)
		}},
		{name: "URL identity too long", change: func(row map[string]json.RawMessage) {
			row["sku"] = json.RawMessage(fmt.Sprintf(
				`"https://example.test/%s"`,
				strings.Repeat("x", documentimport.MaximumMigrationURLBytes),
			))
		}},
		{name: "text too long", change: func(row map[string]json.RawMessage) {
			value, _ := json.Marshal(
				strings.Repeat("x", documentimport.MaximumRecordTextBytes+1),
			)
			row["text_t"] = value
		}},
	})
}

func TestMapSolrDocumentRejectsOversizedHeadingsMetadataAndLinks(t *testing.T) {
	assertSolrDocumentsRejected(t, []solrMappingRejection{
		{
			name: "heading metadata exceeds preservation limit",
			change: func(row map[string]json.RawMessage) {
				row["h1_txt"], _ = json.Marshal([]string{
					strings.Repeat("x", documentimport.MaximumRecordMetadataBytes+1),
				})
			},
		},
		{name: "metadata too large", change: func(row map[string]json.RawMessage) {
			row["description_txt"], _ = json.Marshal(
				strings.Repeat("x", documentimport.MaximumRecordMetadataBytes+1),
			)
		}},
		{
			name: "outbound protocol index out of range",
			change: func(row map[string]json.RawMessage) {
				row["outboundlinks_protocol_sxt"] = json.RawMessage(`["001-https"]`)
				row["outboundlinks_urlstub_sxt"] = json.RawMessage(`[]`)
			},
		},
		{name: "inbound protocol index out of range", change: func(row map[string]json.RawMessage) {
			row["inboundlinks_protocol_sxt"] = json.RawMessage(`["001-https"]`)
			row["inboundlinks_urlstub_sxt"] = json.RawMessage(`[]`)
		}},
	})
}

type solrMappingRejection struct {
	name   string
	change func(map[string]json.RawMessage)
}

func fillSolrLinkArrays(protocols, stubs []string) {
	for index := range protocols {
		protocols[index] = fmt.Sprintf("%03d-https", index)
		stubs[index] = fmt.Sprintf("example.test/%d", index)
	}
}

func assertSolrDocumentsRejected(t *testing.T, tests []solrMappingRejection) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := map[string]json.RawMessage{"sku": json.RawMessage(`"https://example.test/"`)}
			test.change(row)
			if _, err := mapSolrDocument(row, "fixture"); err == nil {
				t.Fatal("unsupported source document was accepted")
			}
		})
	}
}

func TestSolrValueAdaptersHandleScalarMalformedAndUnsupportedValues(t *testing.T) {
	if got := solrString(json.RawMessage(` null `)); got != "" {
		t.Fatalf("null string = %q, want empty", got)
	}
	if got := solrString(json.RawMessage(` 42 `)); got != "42" {
		t.Fatalf("numeric string fallback = %q, want trimmed raw value", got)
	}
	if got := solrStrings(json.RawMessage(`null`)); got != nil {
		t.Fatalf("null string list = %#v, want nil", got)
	}
	if got := solrStrings(
		json.RawMessage(`" single value "`),
	); len(got) != 1 ||
		got[0] != " single value " {
		t.Fatalf("scalar string list = %#v", got)
	}
	if got := solrStrings(
		json.RawMessage(`["kept","",null,7]`),
	); len(got) != 2 || got[0] != "kept" ||
		got[1] != "7" {
		t.Fatalf("filtered string list = %#v", got)
	}
	if got := solrTime(json.RawMessage(`null`)); !got.IsZero() {
		t.Fatalf("missing time = %v, want zero", got)
	}
	if got := solrTime(json.RawMessage(`"not-a-date"`)); !got.IsZero() {
		t.Fatalf("malformed time = %v, want zero", got)
	}
	if got := solrTime(
		json.RawMessage(`"2026-10-01T10:00:00-04:00"`),
	); got.Location() != time.UTC ||
		got.Hour() != 14 {
		t.Fatalf("parsed time = %v, want equivalent UTC", got)
	}
}

func TestSolrLinksRetainSupportedProtocolsAndIgnoreMalformedTargets(t *testing.T) {
	links, available, err := solrLinks(
		json.RawMessage(`["000-ftp","001-https","002-smb","003-https","004-https"]`),
		json.RawMessage(
			`["example.test/file","example.test/page","server/share","example.test/%zz","user:pass@example.test"]`,
		),
		10,
	)
	if err != nil {
		t.Fatalf("solrLinks: %v", err)
	}
	want := []string{
		"ftp://example.test/file",
		"https://example.test/page",
		"smb://server/share",
	}
	if !slices.Equal(links, want) {
		t.Fatalf("filtered links = %#v", links)
	}
	if available != len(want) {
		t.Fatalf("available links = %d, want %d", available, len(want))
	}
}

func TestSolrLinksDecodeSparseProtocolOverrides(t *testing.T) {
	links, available, err := solrLinks(
		json.RawMessage(`["000-https","001-https"]`),
		json.RawMessage(`["secure.example.test/a","secure.example.test/b","plain.example.test/c"]`),
		10,
	)
	if err != nil {
		t.Fatalf("solrLinks: %v", err)
	}
	want := []string{
		"https://secure.example.test/a",
		"https://secure.example.test/b",
		"http://plain.example.test/c",
	}
	if len(links) != len(want) {
		t.Fatalf("decoded links = %#v, want %#v", links, want)
	}
	if available != len(want) {
		t.Fatalf("available links = %d, want %d", available, len(want))
	}
	for index := range want {
		if links[index] != want[index] {
			t.Fatalf("decoded links = %#v, want %#v", links, want)
		}
	}
}
