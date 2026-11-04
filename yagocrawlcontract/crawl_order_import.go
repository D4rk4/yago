package yagocrawlcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	MaximumCrawlOrderImportBatchRecords  = 256
	MaximumCrawlOrderImportBatchBytes    = 4 << 20
	MaximumCrawlOrderImportSourceID      = 96
	MaximumCrawlOrderImportAnchorBytes   = 16 << 10
	MaximumCrawlOrderImportMetadata      = 64
	MaximumCrawlOrderImportMetadataBytes = 1 << 20
)

type CrawlOrderImportBatch struct {
	Version  int            `json:"version"`
	SourceID string         `json:"sourceId"`
	BatchID  string         `json:"batchId"`
	Profile  CrawlProfile   `json:"profile"`
	Requests []CrawlRequest `json:"requests"`
	DryRun   bool           `json:"dryRun,omitempty"`
}

func (b CrawlOrderImportBatch) ComputeBatchID() (string, error) {
	payload := struct {
		Profile  CrawlProfile
		Requests []CrawlRequest
	}{
		Profile:  b.Profile,
		Requests: b.Requests,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal crawl-order import batch identity: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (b CrawlOrderImportBatch) IdempotencyKey() string {
	return "frontier:" + b.SourceID + ":" + b.BatchID
}

func (b CrawlOrderImportBatch) Validate() error {
	if err := validateCrawlOrderImportEnvelope(b); err != nil {
		return err
	}
	if err := validateCrawlOrderImportProfile(b.Profile); err != nil {
		return err
	}
	if err := validateCrawlOrderImportRequests(b); err != nil {
		return err
	}
	computed, err := b.ComputeBatchID()
	if err != nil {
		return err
	}
	if computed != b.BatchID {
		return fmt.Errorf("frontier batch ID does not match payload")
	}
	return nil
}

func validateCrawlOrderImportEnvelope(b CrawlOrderImportBatch) error {
	if b.Version != 1 || len(b.Requests) == 0 ||
		len(b.Requests) > MaximumCrawlOrderImportBatchRecords {
		return fmt.Errorf("unsupported frontier version or request count")
	}
	if len(b.SourceID) == 0 || len(b.SourceID) > MaximumCrawlOrderImportSourceID ||
		strings.TrimSpace(b.SourceID) != b.SourceID || !validImportSourceID(b.SourceID) {
		return fmt.Errorf("invalid frontier source ID")
	}
	if len(b.BatchID) != sha256.Size*2 {
		return fmt.Errorf("invalid frontier batch ID")
	}
	if _, err := hex.DecodeString(
		b.BatchID,
	); err != nil ||
		strings.ToLower(b.BatchID) != b.BatchID {
		return fmt.Errorf("invalid frontier batch ID")
	}
	return nil
}

func validateCrawlOrderImportProfile(profile CrawlProfile) error {
	if len(profile.Handle) == 0 || len(profile.Handle) > MaximumProfileHandleBytes ||
		!utf8.ValidString(profile.Name) || len(profile.Name) > 512 {
		return fmt.Errorf("invalid frontier crawl profile")
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("invalid frontier crawl profile: %w", err)
	}
	if err := validateOptionalImportMetadata(profile.SourceMetadata); err != nil {
		return fmt.Errorf("invalid frontier profile metadata: %w", err)
	}
	return nil
}

func validateCrawlOrderImportRequests(batch CrawlOrderImportBatch) error {
	seen := make(map[string]struct{}, len(batch.Requests))
	for _, request := range batch.Requests {
		if err := validateCrawlOrderImportRequest(batch.Profile, request, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateCrawlOrderImportRequest(
	profile CrawlProfile,
	request CrawlRequest,
	seen map[string]struct{},
) error {
	if err := validateCrawlOrderImportURL(
		request.URL,
		"frontier URL must be an absolute HTTP, HTTPS, FTP, or SMB URL",
	); err != nil {
		return err
	}
	if request.ProfileHandle != profile.Handle || request.Depth < 0 ||
		request.Depth > MaxCrawlDepth || len(request.AnchorName) > MaximumCrawlOrderImportAnchorBytes ||
		!utf8.ValidString(request.AnchorName) {
		return fmt.Errorf("invalid frontier request metadata")
	}
	if request.Mode != "" {
		if _, ok := NormalizeCrawlRequestMode(request.Mode); !ok {
			return fmt.Errorf("invalid frontier request mode")
		}
	}
	if request.ReferrerURL != "" {
		if err := validateCrawlOrderImportURL(
			request.ReferrerURL,
			"invalid frontier referrer URL",
		); err != nil {
			return err
		}
	}
	if request.Initiator != "" && !request.Initiator.Valid() {
		return fmt.Errorf("invalid frontier initiator")
	}
	identity := request.ProfileHandle + "\x00" + request.URL
	if _, duplicate := seen[identity]; duplicate {
		return fmt.Errorf("frontier batch contains duplicate URL/profile pairs")
	}
	seen[identity] = struct{}{}
	if err := validateOptionalImportMetadata(request.SourceMetadata); err != nil {
		return fmt.Errorf("invalid frontier request metadata: %w", err)
	}
	return nil
}

func validateOptionalImportMetadata(metadata *CrawlSourceMetadata) error {
	if metadata == nil {
		return nil
	}
	return validateImportMetadata(map[string]string(*metadata))
}

func validateCrawlOrderImportURL(rawURL, errorMessage string) error {
	if rawURL == "" || len(rawURL) > MaximumCrawlURLBytes || !utf8.ValidString(rawURL) {
		return fmt.Errorf("invalid frontier URL")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		!importableCrawlScheme(parsed.Scheme) {
		return fmt.Errorf("%s", errorMessage)
	}
	return nil
}

func validImportSourceID(value string) bool {
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func importableCrawlScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https", "ftp", "smb":
		return true
	default:
		return false
	}
}

func validateImportMetadata(metadata map[string]string) error {
	if len(metadata) > MaximumCrawlOrderImportMetadata {
		return fmt.Errorf("too many metadata fields")
	}
	bytes := 0
	for key, value := range metadata {
		if key == "" || len(key) > 256 || !utf8.ValidString(key) || !utf8.ValidString(value) {
			return fmt.Errorf("invalid metadata entry")
		}
		bytes += len(key) + len(value)
	}
	if bytes > MaximumCrawlOrderImportMetadataBytes {
		return fmt.Errorf("metadata exceeds size limit")
	}
	return nil
}
