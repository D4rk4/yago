package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentimport"
)

type interchangeHeader struct {
	Type     string `json:"type"`
	Version  int    `json:"version"`
	SourceID string `json:"sourceId"`
}

type interchangeRecord struct {
	Type    string                `json:"type"`
	Version int                   `json:"version"`
	Record  documentimport.Record `json:"record"`
}

const migrationLinkTruncationMetadataKey = "yacy.migration_links_truncated"

func mapSolrDocument(
	source map[string]json.RawMessage,
	sourceID string,
) (documentimport.Record, error) {
	urls, err := mapSolrURLs(source)
	if err != nil {
		return documentimport.Record{}, err
	}
	content, err := mapSolrContent(source)
	if err != nil {
		return documentimport.Record{}, err
	}
	metadata, err := mapSolrMetadata(source)
	if err != nil {
		return documentimport.Record{}, err
	}
	links, err := mapSolrDocumentLinks(source)
	if err != nil {
		return documentimport.Record{}, err
	}
	if err := addSolrPreservationMetadata(metadata, urls, content, links); err != nil {
		return documentimport.Record{}, err
	}
	contentHash := sha256.Sum256([]byte(content.text))
	document := yagocrawlcontract.DocumentIngest{
		CanonicalURL:  urls.canonical,
		NormalizedURL: urls.normalized,
		Title:         content.title,
		Headings:      content.headings,
		ExtractedText: content.text,
		Language:      solrString(source["language_s"]),
		ContentType:   solrFirstString(source["content_type"]),
		FetchStatus:   solrString(source["httpstatus_i"]),
		FetchedAt:     solrTime(source["load_date_dt"]),
		ModifiedAt:    solrTime(source["last_modified"]),
		ContentHash:   hex.EncodeToString(contentHash[:]),
		Outlinks:      links.outbound,
		Inlinks:       links.inbound,
		Metadata:      metadata,
	}
	nextVisitAt := solrTimePointer(source["fresh_date_dt"])
	if !isHTTPURL(urls.normalized) || urls.credentialsRedacted || urls.legacyURLStatus != "" {
		nextVisitAt = nil
	}
	id := solrString(source["id"])
	if id == "" {
		id = urls.normalized + ":" + document.ContentHash
	}
	idHash := sha256.Sum256([]byte(sourceID + "\x00" + id))
	return documentimport.Record{
		ID:          hex.EncodeToString(idHash[:]),
		Document:    document,
		NextVisitAt: nextVisitAt,
	}, nil
}

func addSolrPreservationMetadata(
	metadata map[string]string,
	urls solrDocumentURLs,
	content solrDocumentContent,
	links solrDocumentLinks,
) error {
	if urls.legacyURLStatus != "" {
		metadata[documentimport.LegacyURLStatusMetadataKey] = urls.legacyURLStatus
	}
	if urls.credentialsRedacted {
		metadata["yacy.migration_credentials_redacted"] = "true"
	}
	if err := addFullTitleMetadata(metadata, content.fullTitle); err != nil {
		return err
	}
	addFullHeadingMetadata(metadata, content.fullHeadings)
	addLinkTruncationMetadata(metadata, links)
	return validateMigrationMetadata(metadata)
}

func addFullTitleMetadata(metadata map[string]string, title string) error {
	if title == "" {
		return nil
	}
	if len(title) > documentimport.MaximumRecordMetadataBytes-len("yacy.title_full") {
		return errors.New("YaCy title exceeds the migration metadata preservation limit")
	}
	metadata["yacy.title_full"] = title
	metadata["yacy.migration_title_truncated"] = "true"
	return nil
}

func addFullHeadingMetadata(metadata map[string]string, headings []solrFullHeading) {
	if len(headings) == 0 {
		return
	}
	encoded, _ := json.Marshal(headings)
	metadata["yacy.headings_full"] = string(encoded)
	metadata["yacy.migration_headings_truncated"] = "true"
}

func addLinkTruncationMetadata(metadata map[string]string, links solrDocumentLinks) {
	if links.outboundAvailable <= len(links.outbound) &&
		links.inboundAvailable <= len(links.inbound) {
		return
	}
	truncation, _ := json.Marshal(migrationLinkTruncation{
		Outbound: migrationLinkCount{
			Available: links.outboundAvailable,
			Stored:    len(links.outbound),
		},
		Inbound: migrationLinkCount{
			Available: links.inboundAvailable,
			Stored:    len(links.inbound),
		},
	})
	metadata[migrationLinkTruncationMetadataKey] = string(truncation)
}

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

type solrDocumentURLs struct {
	normalized          string
	canonical           string
	legacyURLStatus     string
	credentialsRedacted bool
}

func mapSolrURLs(source map[string]json.RawMessage) (solrDocumentURLs, error) {
	primaryURL := solrString(source["sku"])
	if primaryURL == "" {
		return solrDocumentURLs{}, errors.New("YaCy row has no sku URL")
	}
	normalizedURL, legacyURLStatus, credentialsRedacted, err := normalizeSolrPrimaryURL(primaryURL)
	if err != nil {
		return solrDocumentURLs{}, err
	}
	canonicalInput := solrString(source["canonical_s"])
	if canonicalInput == "" {
		canonicalInput = normalizedURL
	}
	canonicalURL, canonicalStatus, canonicalRedacted, err := normalizeSolrCanonicalURL(
		canonicalInput,
	)
	if err != nil {
		return solrDocumentURLs{}, err
	}
	if canonicalStatus != "" {
		legacyURLStatus = canonicalStatus
	}
	credentialsRedacted = credentialsRedacted || canonicalRedacted
	if len(normalizedURL) > documentimport.MaximumMigrationURLBytes {
		return solrDocumentURLs{}, errors.New("YaCy URL exceeds Yago's URL identity limit")
	}
	return solrDocumentURLs{
		normalized: normalizedURL, canonical: canonicalURL, legacyURLStatus: legacyURLStatus,
		credentialsRedacted: credentialsRedacted,
	}, nil
}

func normalizeSolrPrimaryURL(raw string) (string, string, bool, error) {
	parsed, err := url.Parse(raw)
	legacyStatus := documentimport.PreservableLegacyURLStatus(raw)
	redacted := false
	if err != nil && legacyStatus == "" {
		if clean, changed := redactMalformedURLUserinfo(raw); changed {
			raw, redacted = clean, true
			parsed, err = url.Parse(raw)
			legacyStatus = documentimport.PreservableLegacyURLStatus(raw)
		}
	}
	if err != nil {
		if legacyStatus == "" {
			return "", "", false, fmt.Errorf(
				"YaCy sku is not a valid absolute URL (%s; %s)",
				safeURLParseError(err),
				safeURLParseContext(raw),
			)
		}
		return raw, legacyStatus, redacted, nil
	}
	if !supportedImportedScheme(parsed.Scheme) {
		return "", "", false, errors.New("YaCy sku uses an unsupported URL scheme")
	}
	if parsed.Host == "" {
		return "", "", false, errors.New("YaCy sku has no host")
	}
	if parsed.User != nil {
		parsed.User = nil
		redacted = true
	}
	parsed.Fragment = ""
	return parsed.String(), legacyStatus, redacted, nil
}

func normalizeSolrCanonicalURL(raw string) (string, string, bool, error) {
	parsed, err := url.Parse(raw)
	redacted := false
	if err != nil {
		if clean, changed := redactMalformedURLUserinfo(raw); changed {
			raw, redacted = clean, true
			parsed, err = url.Parse(raw)
		}
	}
	if err != nil {
		legacyStatus := documentimport.PreservableLegacyURLStatus(raw)
		if legacyStatus == "" {
			return "", "", false, errors.New(
				"YaCy canonical URL is not a supported credential-free absolute URL",
			)
		}
		return raw, legacyStatus, redacted, nil
	}
	if parsed.User != nil {
		parsed.User = nil
		raw, redacted = parsed.String(), true
	}
	legacyStatus := documentimport.PreservableLegacyURLStatus(raw)
	valid := supportedImportedScheme(parsed.Scheme) && parsed.Host != "" &&
		parsed.User == nil && parsed.Fragment == ""
	if !valid && legacyStatus == "" {
		return "", "", false, errors.New(
			"YaCy canonical URL is not a supported credential-free absolute URL",
		)
	}
	return raw, legacyStatus, redacted, nil
}

func redactMalformedURLUserinfo(rawURL string) (string, bool) {
	separator := strings.Index(rawURL, "://")
	if separator < 0 {
		return "", false
	}
	scheme := strings.ToLower(rawURL[:separator])
	if !supportedImportedScheme(scheme) {
		return "", false
	}
	authorityStart := separator + 3
	authorityEnd := len(rawURL)
	if relativeEnd := strings.IndexAny(rawURL[authorityStart:], "/?#"); relativeEnd >= 0 {
		authorityEnd = authorityStart + relativeEnd
	}
	authority := rawURL[authorityStart:authorityEnd]
	userinfoEnd := strings.LastIndexByte(authority, '@')
	if userinfoEnd <= 0 || userinfoEnd == len(authority)-1 {
		return "", false
	}
	return rawURL[:authorityStart] + authority[userinfoEnd+1:] + rawURL[authorityEnd:], true
}

func safeURLParseError(err error) string {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) || urlErr.Err == nil {
		return "other parser rejection"
	}
	var escapeError url.EscapeError
	if errors.As(urlErr.Err, &escapeError) {
		if documentimport.PreservableLegacyURLStatus(urlErr.URL) ==
			documentimport.LegacyURLStatusEncodedHostWhitespace {
			return "percent-encoded whitespace in host"
		}
		return "invalid percent escape"
	}
	var invalidHostError url.InvalidHostError
	if errors.As(urlErr.Err, &invalidHostError) {
		return "invalid host character"
	}
	message := strings.ToLower(urlErr.Err.Error())
	switch {
	case strings.Contains(message, "invalid control character"):
		return "control character"
	case strings.Contains(message, "invalid port"):
		return "invalid port"
	case strings.Contains(message, "missing ']' in host"):
		return "malformed bracketed host"
	default:
		return "other parser rejection"
	}
}

func safeURLParseContext(rawURL string) string {
	if strings.IndexFunc(rawURL, unicode.IsControl) >= 0 {
		return "control character in source URL"
	}
	if strings.ContainsAny(rawURL, "#\\") {
		return "fragment or backslash in source URL"
	}
	separator := strings.Index(rawURL, "://")
	if separator < 0 {
		return "missing scheme-authority separator"
	}
	if !supportedImportedScheme(rawURL[:separator]) {
		return "unsupported source scheme"
	}
	authorityStart := separator + 3
	authorityEnd := len(rawURL)
	if relativeEnd := strings.IndexAny(rawURL[authorityStart:], "/?"); relativeEnd >= 0 {
		authorityEnd = authorityStart + relativeEnd
	}
	authority := rawURL[authorityStart:authorityEnd]
	if authority == "" {
		return "empty source authority"
	}
	if strings.Contains(authority, "@") {
		return "userinfo-bearing source authority"
	}
	if strings.Contains(authority, "%") {
		if hasMalformedPercentEscape(authority) {
			return "malformed percent escape in source authority"
		}
		return "percent escape rejected in source authority"
	}
	if hasMalformedPercentEscape(rawURL[authorityEnd:]) {
		return "malformed percent escape after source authority"
	}
	return fmt.Sprintf(
		"unclassified source URL shape (bytes=%d; percent_signs=%d; malformed_percent=%t)",
		len(rawURL), strings.Count(rawURL, "%"), hasMalformedPercentEscape(rawURL),
	)
}

func hasMalformedPercentEscape(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			continue
		}
		if index+2 >= len(value) || !isHexDigit(value[index+1]) || !isHexDigit(value[index+2]) {
			return true
		}
		index += 2
	}
	return false
}

func isHexDigit(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f') ||
		(value >= 'A' && value <= 'F')
}

func supportedImportedScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https", "ftp", "smb":
		return true
	default:
		return false
	}
}

type solrDocumentContent struct {
	title        string
	fullTitle    string
	text         string
	headings     []string
	fullHeadings []solrFullHeading
}

type solrFullHeading struct {
	Field string `json:"field"`
	Index int    `json:"index"`
	Text  string `json:"text"`
}

func mapSolrContent(source map[string]json.RawMessage) (solrDocumentContent, error) {
	text := strings.Join(solrStrings(source["text_t"]), "\n")
	if len(text) > documentimport.MaximumRecordTextBytes {
		return solrDocumentContent{}, fmt.Errorf(
			"YaCy document text is %d bytes; migration import accepts at most %d",
			len(text),
			documentimport.MaximumRecordTextBytes,
		)
	}
	headings := make([]string, 0)
	fullHeadings := make([]solrFullHeading, 0)
	for level := 1; level <= 6; level++ {
		field := fmt.Sprintf("h%d_txt", level)
		values := solrStrings(source[field])
		for index, heading := range values {
			if len(headings) >= documentimport.MaximumRecordHeadings {
				fullHeadings = append(fullHeadings, solrFullHeading{
					Field: field,
					Index: index,
					Text:  heading,
				})
				continue
			}
			if len(heading) > documentimport.MaximumRecordHeadingBytes {
				fullHeadings = append(fullHeadings, solrFullHeading{
					Field: field,
					Index: index,
					Text:  heading,
				})
				heading = truncateUTF8(heading, documentimport.MaximumRecordHeadingBytes)
			}
			headings = append(headings, heading)
		}
	}
	title := solrString(source["title"])
	if len(title) > yagocrawlcontract.MaximumDocumentTitleBytes {
		return solrDocumentContent{
			title:        truncateUTF8(title, yagocrawlcontract.MaximumDocumentTitleBytes),
			fullTitle:    title,
			text:         text,
			headings:     headings,
			fullHeadings: fullHeadings,
		}, nil
	}
	return solrDocumentContent{
		title: title, text: text, headings: headings, fullHeadings: fullHeadings,
	}, nil
}

func validateMigrationMetadata(metadata map[string]string) error {
	metadataBytes := 0
	for key, value := range metadata {
		metadataBytes += len(key) + len(value)
	}
	if len(metadata) > yagocrawlcontract.MaximumDocumentMetadata ||
		metadataBytes > documentimport.MaximumRecordMetadataBytes {
		return errors.New("YaCy migration metadata exceeds Yago's bounded preservation limit")
	}
	return nil
}

func truncateUTF8(value string, maximumBytes int) string {
	if len(value) <= maximumBytes {
		return value
	}
	for maximumBytes > 0 && !isUTF8Boundary(value, maximumBytes) {
		maximumBytes--
	}
	return value[:maximumBytes]
}

func isUTF8Boundary(value string, index int) bool {
	return index == len(value) || index == 0 || value[index]&0xc0 != 0x80
}

func mapSolrMetadata(source map[string]json.RawMessage) (map[string]string, error) {
	metadata := make(map[string]string)
	for sourceField, targetField := range map[string]string{
		"collection_sxt":              "yacy.collections",
		"exact_signature_l":           "yacy.exact_signature",
		"focused_policy_sxt":          "yacy.focused_policy_ids",
		"focused_profile_version_sxt": "yacy.focused_profile_versions",
		"focused_relevance_i":         "yacy.focused_relevance",
		"focused_priority_s":          "yacy.focused_priority",
		"focused_reason_sxt":          "yacy.focused_reasons",
		"failreason_s":                "yacy.failure_reason",
		"failtype_s":                  "yacy.failure_type",
		"description_txt":             "yacy.description",
		"fresh_date_dt":               "yacy.fresh_until",
	} {
		if raw := source[sourceField]; len(raw) > 0 && string(raw) != "null" {
			metadata[targetField] = string(raw)
		}
	}
	mapSolrRedirectMetadata(source, metadata)
	mapSolrSourceIDMetadata(source, metadata)
	if err := validateSolrMetadataBounds(metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func mapSolrRedirectMetadata(source map[string]json.RawMessage, metadata map[string]string) {
	if raw := source["httpstatus_redirect_s"]; len(raw) > 0 && string(raw) != "null" {
		var value any
		if err := json.Unmarshal(raw, &value); err == nil {
			clean, redacted := redactURLCredentials(value)
			encoded, _ := json.Marshal(clean)
			metadata["yacy.redirect"] = string(encoded)
			if redacted {
				metadata["yacy.migration_credentials_redacted"] = "true"
			}
		}
	}
}

func mapSolrSourceIDMetadata(source map[string]json.RawMessage, metadata map[string]string) {
	if yacyID := solrString(source["id"]); yacyID != "" {
		cleanID, redacted := redactURLCredentials(yacyID)
		metadata["yacy.source_id"] = cleanID.(string)
		if redacted {
			metadata["yacy.migration_credentials_redacted"] = "true"
		}
	}
}

func validateSolrMetadataBounds(metadata map[string]string) error {
	metadataBytes := 0
	for key, value := range metadata {
		metadataBytes += len(key) + len(value)
	}
	if len(metadata) > yagocrawlcontract.MaximumDocumentMetadata ||
		metadataBytes > documentimport.MaximumRecordMetadataBytes {
		return errors.New(
			"YaCy metadata exceeds Yago's document metadata limits",
		)
	}
	return nil
}

func redactURLCredentials(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		parsed, err := url.Parse(typed)
		if err != nil || parsed.User == nil || parsed.Host == "" {
			return typed, false
		}
		parsed.User = nil
		return parsed.String(), true
	case []any:
		redacted := false
		clean := make([]any, len(typed))
		for index, item := range typed {
			var itemRedacted bool
			clean[index], itemRedacted = redactURLCredentials(item)
			redacted = redacted || itemRedacted
		}
		return clean, redacted
	case map[string]any:
		redacted := false
		clean := make(map[string]any, len(typed))
		for key, item := range typed {
			var itemRedacted bool
			clean[key], itemRedacted = redactURLCredentials(item)
			redacted = redacted || itemRedacted
		}
		return clean, redacted
	default:
		return value, false
	}
}

type solrDocumentLinks struct {
	outbound          []string
	outboundAvailable int
	inbound           []yagocrawlcontract.AnchorText
	inboundAvailable  int
}

type migrationLinkCount struct {
	Available int `json:"available"`
	Stored    int `json:"stored"`
}

type migrationLinkTruncation struct {
	Outbound migrationLinkCount `json:"outbound"`
	Inbound  migrationLinkCount `json:"inbound"`
}

func mapSolrDocumentLinks(source map[string]json.RawMessage) (solrDocumentLinks, error) {
	links, availableOutlinks, err := solrLinks(
		source["outboundlinks_protocol_sxt"],
		source["outboundlinks_urlstub_sxt"],
		documentimport.MaximumRecordOutlinks,
	)
	if err != nil {
		return solrDocumentLinks{}, fmt.Errorf("map YaCy outbound links: %w", err)
	}
	inlinks, availableInlinks, err := solrLinks(
		source["inboundlinks_protocol_sxt"],
		source["inboundlinks_urlstub_sxt"],
		documentimport.MaximumRecordInlinks,
	)
	if err != nil {
		return solrDocumentLinks{}, fmt.Errorf("map YaCy inbound links: %w", err)
	}
	inbound := make([]yagocrawlcontract.AnchorText, 0, len(inlinks))
	for _, link := range inlinks {
		inbound = append(inbound, yagocrawlcontract.AnchorText{URL: link})
	}
	return solrDocumentLinks{
		outbound: links, outboundAvailable: availableOutlinks,
		inbound: inbound, inboundAvailable: availableInlinks,
	}, nil
}

func solrString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return strings.TrimSpace(string(raw))
}

func solrStrings(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err == nil {
		out := make([]string, 0, len(values))
		for _, value := range values {
			if item := solrString(value); item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	return []string{solrString(raw)}
}

func solrFirstString(raw json.RawMessage) string {
	values := solrStrings(raw)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func solrLinks(protocolsRaw, stubsRaw json.RawMessage, maximum int) ([]string, int, error) {
	protocolEntries := solrStringPositions(protocolsRaw)
	stubs := solrStringPositions(stubsRaw)
	protocols := make([]string, len(stubs))
	for index := range protocols {
		protocols[index] = "http"
	}
	seen := make([]bool, len(stubs))
	for _, entry := range protocolEntries {
		separator := strings.IndexByte(entry, '-')
		if separator <= 0 || separator == len(entry)-1 {
			return nil, 0, errors.New("invalid indexed protocol entry")
		}
		index, err := strconv.Atoi(entry[:separator])
		if err != nil || index < 0 || index >= len(stubs) {
			return nil, 0, errors.New("indexed protocol entry is outside the URL-stub array")
		}
		if seen[index] {
			return nil, 0, errors.New("duplicate indexed protocol entry")
		}
		protocols[index] = strings.ToLower(strings.TrimSuffix(entry[separator+1:], ":"))
		seen[index] = true
	}
	links := make([]string, 0, min(len(stubs), maximum))
	available := 0
	for index, stub := range stubs {
		protocol := protocols[index]
		if !supportedImportedScheme(protocol) {
			continue
		}
		candidate := protocol + "://" + strings.TrimLeft(stub, "/")
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Host == "" || parsed.User != nil {
			continue
		}
		available++
		if len(links) < maximum {
			links = append(links, parsed.String())
		}
	}
	return links, available, nil
}

func solrStringPositions(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{solrString(raw)}
	}
	positions := make([]string, len(values))
	for index, value := range values {
		if string(value) != "null" {
			positions[index] = solrString(value)
		}
	}
	return positions
}

func solrTime(raw json.RawMessage) time.Time {
	value := solrString(raw)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func solrTimePointer(raw json.RawMessage) *time.Time {
	parsed := solrTime(raw)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}
