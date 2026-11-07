package yagocrawlcontract

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type legacyCrawlRequest struct {
	URL           string
	Mode          CrawlRequestMode
	ReferrerURL   string
	AnchorName    string
	Depth         int
	ProfileHandle string
	Initiator     yagomodel.Hash
	AppDate       time.Time
	LastModified  time.Time
}

type legacyCrawlOrder struct {
	Provenance []byte
	Priority   CrawlOrderPriority
	Profile    CrawlProfile
	Requests   []legacyCrawlRequest
}

type legacyIngestBatch struct {
	SourceURL        string
	Provenance       []byte
	ProfileHandle    string
	ObservationID    string
	ObservedAt       time.Time
	SourceModifiedAt time.Time
	Removed          bool
	Document         DocumentIngest
	Postings         []yagomodel.RWIPosting
	Metadata         []yagomodel.URIMetadataRow
}

func legacyPageRequest() legacyCrawlRequest {
	return legacyCrawlRequest{
		URL:           "https://example.org/a",
		Mode:          CrawlRequestModeURL,
		Depth:         1,
		ProfileHandle: "profile-a",
		LastModified:  time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC),
	}
}

func currentPageRequest(fetch *ConditionalFetch) CrawlRequest {
	legacy := legacyPageRequest()

	return CrawlRequest{
		URL:              legacy.URL,
		Mode:             legacy.Mode,
		Depth:            legacy.Depth,
		ProfileHandle:    legacy.ProfileHandle,
		LastModified:     legacy.LastModified,
		ConditionalFetch: fetch,
	}
}

func TestCrawlOrderWithoutConditionalFetchEncodesLikeLegacyOrder(t *testing.T) {
	legacy, err := json.Marshal(legacyCrawlOrder{
		Provenance: []byte("order-a"),
		Requests:   []legacyCrawlRequest{legacyPageRequest()},
	})
	if err != nil {
		t.Fatalf("marshal legacy order: %v", err)
	}
	current, err := MarshalCrawlOrder(CrawlOrder{
		Provenance: []byte("order-a"),
		Requests:   []CrawlRequest{currentPageRequest(nil)},
	})
	if err != nil {
		t.Fatalf("marshal current order: %v", err)
	}
	if !bytes.Equal(legacy, current) {
		t.Fatalf("unconditional order encoding changed:\nlegacy  %s\ncurrent %s", legacy, current)
	}
	decoded, err := UnmarshalCrawlOrder(legacy)
	if err != nil {
		t.Fatalf("decode legacy order: %v", err)
	}
	if decoded.Requests[0].ConditionalFetch != nil {
		t.Fatalf(
			"legacy request decoded with conditional fetch %+v",
			decoded.Requests[0].ConditionalFetch,
		)
	}
	if err := decoded.Requests[0].ValidateConditionalFetch(); err != nil {
		t.Fatalf("legacy request refused: %v", err)
	}
}

func TestConditionalFetchRoundTripsUnchanged(t *testing.T) {
	fetch := issuedConditionalFetch()
	fetch.Validators.ETag = `W/"<&>\"`
	order := CrawlOrder{Requests: []CrawlRequest{currentPageRequest(fetch)}}
	data, err := MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal conditional order: %v", err)
	}
	decoded, err := UnmarshalCrawlOrder(data)
	if err != nil {
		t.Fatalf("decode conditional order: %v", err)
	}
	got := decoded.Requests[0].ConditionalFetch
	if got == nil {
		t.Fatal("conditional fetch was lost")
	}
	if got.Validators != fetch.Validators {
		t.Fatalf("validators = %+v, want %+v", got.Validators, fetch.Validators)
	}
	if !got.Revision.Matches(fetch.Revision) ||
		got.Revision.FetchedAt.Nanosecond() != fetch.Revision.FetchedAt.Nanosecond() {
		t.Fatalf("revision = %+v, want %+v", got.Revision, fetch.Revision)
	}
	if err := decoded.Requests[0].ValidateConditionalFetch(); err != nil {
		t.Fatalf("round-tripped conditional fetch refused: %v", err)
	}
}

func TestLegacyCrawlerDecodesConditionalOrderAsUnconditionalWork(t *testing.T) {
	data, err := MarshalCrawlOrder(CrawlOrder{
		Provenance: []byte("order-a"),
		Requests:   []CrawlRequest{currentPageRequest(issuedConditionalFetch())},
	})
	if err != nil {
		t.Fatalf("marshal conditional order: %v", err)
	}
	var legacy legacyCrawlOrder
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatalf("legacy crawler refused conditional order: %v", err)
	}
	if !reflect.DeepEqual(legacy.Requests, []legacyCrawlRequest{legacyPageRequest()}) {
		t.Fatalf("legacy crawler request = %+v, want %+v", legacy.Requests, legacyPageRequest())
	}
}

func TestIngestBatchWithoutConditionalRecrawlEncodesLikeLegacyBatch(t *testing.T) {
	observedAt := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	legacy, err := json.Marshal(legacyIngestBatch{
		SourceURL:     "https://example.org/a",
		ObservationID: "observation-a",
		ObservedAt:    observedAt,
		Removed:       true,
	})
	if err != nil {
		t.Fatalf("marshal legacy batch: %v", err)
	}
	current, err := MarshalIngestBatch(IngestBatch{
		SourceURL:     "https://example.org/a",
		ObservationID: "observation-a",
		ObservedAt:    observedAt,
		Removed:       true,
	})
	if err != nil {
		t.Fatalf("marshal current batch: %v", err)
	}
	if !bytes.Equal(legacy, current) {
		t.Fatalf("legacy batch encoding changed:\nlegacy  %s\ncurrent %s", legacy, current)
	}
	decoded, err := UnmarshalIngestBatch(legacy)
	if err != nil {
		t.Fatalf("decode legacy batch: %v", err)
	}
	if decoded.NotModified != nil || decoded.ResponseValidators != nil {
		t.Fatalf("legacy batch decoded with conditional recrawl facts: %+v", decoded)
	}
	if err := decoded.ValidatePayload(); err != nil {
		t.Fatalf("legacy batch refused: %v", err)
	}
}

func TestNotModifiedBatchRoundTripsUnchanged(t *testing.T) {
	batch := notModifiedBatch()
	data, err := MarshalIngestBatch(batch)
	if err != nil {
		t.Fatalf("marshal not-modified batch: %v", err)
	}
	decoded, err := UnmarshalIngestBatch(data)
	if err != nil {
		t.Fatalf("decode not-modified batch: %v", err)
	}
	if *decoded.ResponseValidators != *batch.ResponseValidators {
		t.Fatalf("response validators = %+v, want %+v",
			decoded.ResponseValidators, batch.ResponseValidators)
	}
	if decoded.NotModified == nil ||
		!decoded.NotModified.Revision.Matches(batch.NotModified.Revision) {
		t.Fatalf("not-modified observation = %+v, want %+v", decoded.NotModified, batch.NotModified)
	}
	if err := decoded.ValidatePayload(); err != nil {
		t.Fatalf("round-tripped not-modified batch refused: %v", err)
	}
}

func TestLegacyNodeDecodesNotModifiedBatchAsSourceOnlyBatch(t *testing.T) {
	data, err := MarshalIngestBatch(notModifiedBatch())
	if err != nil {
		t.Fatalf("marshal not-modified batch: %v", err)
	}
	var legacy legacyIngestBatch
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatalf("legacy node refused not-modified batch: %v", err)
	}
	if legacy.SourceURL != "https://example.org/a" || legacy.Removed ||
		!reflect.DeepEqual(legacy.Document, DocumentIngest{}) {
		t.Fatalf("legacy node batch = %+v, want a source-only batch", legacy)
	}
}
