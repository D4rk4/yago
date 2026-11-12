package yagocrawlcontract

import (
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestCrawlOrderImportBatchIdentityIsStableAndContentBound(t *testing.T) {
	base := CrawlOrderImportBatch{
		Version:  1,
		SourceID: "source-a",
		Profile: CrawlProfile{
			Handle: "profile-a",
			SourceMetadata: NewCrawlSourceMetadata(map[string]string{
				"yacy.depth": "2", "yacy.name": "source",
			}),
		},
		Requests: []CrawlRequest{{
			URL:           "https://example.test/a",
			ProfileHandle: "profile-a",
			SourceMetadata: NewCrawlSourceMetadata(map[string]string{
				"yacy.stack": "LOCAL",
			}),
		}},
	}
	first, err := base.ComputeBatchID()
	if err != nil {
		t.Fatalf("compute base batch ID: %v", err)
	}
	base.BatchID = first

	mapOrderChanged := base
	mapOrderChanged.Profile.SourceMetadata = NewCrawlSourceMetadata(map[string]string{
		"yacy.name":  "source",
		"yacy.depth": "2",
	})
	mapOrderChanged.SourceID = "source-b"
	mapOrderChanged.DryRun = true
	second, err := mapOrderChanged.ComputeBatchID()
	if err != nil {
		t.Fatalf("compute equivalent batch ID: %v", err)
	}
	if first != second {
		t.Fatalf("equivalent payload IDs differ: %q != %q", first, second)
	}
	if got := base.IdempotencyKey(); got != "frontier:source-a:"+first {
		t.Fatalf("idempotency key = %q", got)
	}

	changed := base
	changed.Requests = append([]CrawlRequest(nil), base.Requests...)
	changed.Requests[0].URL = "https://example.test/b"
	third, err := changed.ComputeBatchID()
	if err != nil {
		t.Fatalf("compute changed batch ID: %v", err)
	}
	if first == third {
		t.Fatal("different crawl request reused the same batch ID")
	}
}

func TestCrawlOrderImportBatchValidateChecksIdentityAndBounds(t *testing.T) {
	batch := CrawlOrderImportBatch{
		Version:  1,
		SourceID: "source-a",
		Profile: CrawlProfile{
			Handle:          "profile-a",
			Name:            "fixture profile",
			URLMustMatch:    MatchAll,
			MaxPagesPerHost: UnlimitedPagesPerHost,
		},
		Requests: []CrawlRequest{{
			URL:           "https://example.test/page",
			ProfileHandle: "profile-a",
			Depth:         2,
		}},
	}
	batch.BatchID, _ = batch.ComputeBatchID()
	if err := batch.Validate(); err != nil {
		t.Fatalf("valid import batch rejected: %v", err)
	}

	changed := batch
	changed.Requests = append([]CrawlRequest(nil), batch.Requests...)
	changed.Requests[0].URL = "https://example.test/other"
	if err := changed.Validate(); err == nil {
		t.Fatal("payload with stale batch ID was accepted")
	}

	changed = batch
	changed.Requests = append([]CrawlRequest(nil), batch.Requests...)
	changed.Requests[0].URL = "https://user:secret@example.test/private"
	changed.BatchID, _ = changed.ComputeBatchID()
	if err := changed.Validate(); err == nil {
		t.Fatal("URL containing user credentials was accepted")
	}

	changed = batch
	changed.Requests = append([]CrawlRequest(nil), batch.Requests...)
	changed.Requests[0].Depth = MaxCrawlDepth + 1
	changed.BatchID, _ = changed.ComputeBatchID()
	if err := changed.Validate(); err == nil {
		t.Fatal("request depth above the supported maximum was accepted")
	}
}

func TestCrawlOrderImportBatchValidationRejectsUnsupportedInputs(t *testing.T) {
	base := validCrawlOrderImportBatch()
	for _, test := range crawlOrderImportRejectedBatchCases() {
		t.Run(test.name, func(t *testing.T) {
			batch := base
			batch.Requests = append([]CrawlRequest(nil), base.Requests...)
			test.mutate(&batch)
			if !test.keepID {
				batch.BatchID, _ = batch.ComputeBatchID()
			}
			if err := batch.Validate(); err == nil {
				t.Fatal("unsupported frontier input was accepted")
			}
		})
	}
}

type crawlOrderImportBatchCase struct {
	name   string
	mutate func(*CrawlOrderImportBatch)
	keepID bool
}

func crawlOrderImportRejectedBatchCases() []crawlOrderImportBatchCase {
	var cases []crawlOrderImportBatchCase
	for _, group := range [][]crawlOrderImportBatchCase{
		crawlOrderImportIdentityCases(),
		crawlOrderImportProfileCases(),
		crawlOrderImportProfileMetadataCases(),
		crawlOrderImportURLCases(),
		crawlOrderImportRequestCases(),
		crawlOrderImportRequestMetadataCases(),
	} {
		cases = append(cases, group...)
	}
	return cases
}

func crawlOrderImportIdentityCases() []crawlOrderImportBatchCase {
	return []crawlOrderImportBatchCase{
		{name: "version", mutate: func(batch *CrawlOrderImportBatch) { batch.Version++ }},
		{
			name:   "empty request list",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests = nil },
		},
		{
			name: "request count limit",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Requests = make([]CrawlRequest, MaximumCrawlOrderImportBatchRecords+1)
			},
		},
		{
			name:   "empty source ID",
			mutate: func(batch *CrawlOrderImportBatch) { batch.SourceID = "" },
		},
		{
			name:   "trimmed source ID",
			mutate: func(batch *CrawlOrderImportBatch) { batch.SourceID = " source" },
		},
		{
			name:   "invalid source ID character",
			mutate: func(batch *CrawlOrderImportBatch) { batch.SourceID = "source/1" },
		},
		{
			name: "source ID limit",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.SourceID = strings.Repeat("s", MaximumCrawlOrderImportSourceID+1)
			},
		},
		{
			name:   "batch ID length",
			keepID: true,
			mutate: func(batch *CrawlOrderImportBatch) { batch.BatchID = "short" },
		},
		{
			name:   "batch ID hex",
			keepID: true,
			mutate: func(batch *CrawlOrderImportBatch) { batch.BatchID = strings.Repeat("z", 64) },
		},
		{
			name:   "batch ID lowercase",
			keepID: true,
			mutate: func(batch *CrawlOrderImportBatch) { batch.BatchID = strings.Repeat("A", 64) },
		},
	}
}

func crawlOrderImportProfileCases() []crawlOrderImportBatchCase {
	return []crawlOrderImportBatchCase{
		{
			name: "profile handle missing",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Profile.Handle = ""
				batch.Requests[0].ProfileHandle = ""
			},
		},
		{
			name: "profile handle limit",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Profile.Handle = strings.Repeat("p", MaximumProfileHandleBytes+1)
				batch.Requests[0].ProfileHandle = batch.Profile.Handle
			},
		},
		{
			name:   "profile name encoding",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Profile.Name = string([]byte{0xff}) },
		},
		{
			name:   "profile name limit",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Profile.Name = strings.Repeat("n", 513) },
		},
		{
			name:   "invalid profile rules",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Profile.MaxDepth = MaxCrawlDepth + 1 },
		},
	}
}

func crawlOrderImportProfileMetadataCases() []crawlOrderImportBatchCase {
	return []crawlOrderImportBatchCase{
		{
			name: "too many profile metadata fields",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Profile.SourceMetadata = NewCrawlSourceMetadata(metadataOverflowValues())
			},
		},
		{
			name: "invalid profile metadata key",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Profile.SourceMetadata = NewCrawlSourceMetadata(
					map[string]string{"": "value"},
				)
			},
		},
		{
			name: "oversized profile metadata key",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Profile.SourceMetadata = NewCrawlSourceMetadata(map[string]string{
					strings.Repeat("k", 257): "value",
				})
			},
		},
		{
			name: "invalid profile metadata encoding",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Profile.SourceMetadata = NewCrawlSourceMetadata(
					map[string]string{"key": string([]byte{0xff})},
				)
			},
		},
		{
			name: "oversized profile metadata",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Profile.SourceMetadata = NewCrawlSourceMetadata(map[string]string{
					"key": strings.Repeat("v", MaximumCrawlOrderImportMetadataBytes),
				})
			},
		},
	}
}

func crawlOrderImportURLCases() []crawlOrderImportBatchCase {
	return []crawlOrderImportBatchCase{
		{
			name:   "empty request URL",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].URL = "" },
		},
		{
			name: "request URL byte limit",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Requests[0].URL = "https://example.test/" + strings.Repeat(
					"x",
					MaximumCrawlURLBytes,
				)
			},
		},
		{
			name:   "request URL encoding",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].URL = string([]byte{0xff}) },
		},
		{
			name:   "relative request URL",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].URL = "/relative" },
		},
		{
			name:   "request URL credentials",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].URL = "https://user:secret@example.test/" },
		},
		{
			name:   "request URL fragment",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].URL = "https://example.test/page#section" },
		},
		{
			name:   "unsupported request scheme",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].URL = "file://example.test/page" },
		},
	}
}

func crawlOrderImportRequestCases() []crawlOrderImportBatchCase {
	return []crawlOrderImportBatchCase{
		{
			name:   "invalid request metadata profile",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].ProfileHandle = "other" },
		},
		{
			name:   "negative depth",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].Depth = -1 },
		},
		{
			name:   "depth limit",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].Depth = MaxCrawlDepth + 1 },
		},
		{
			name: "anchor size limit",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Requests[0].AnchorName = strings.Repeat(
					"a",
					MaximumCrawlOrderImportAnchorBytes+1,
				)
			},
		},
		{
			name:   "anchor encoding",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].AnchorName = string([]byte{0xff}) },
		},
		{
			name:   "request mode",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].Mode = "unknown" },
		},
		{
			name:   "invalid referrer",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].ReferrerURL = "file:///tmp/referrer" },
		},
		{
			name:   "invalid initiator",
			mutate: func(batch *CrawlOrderImportBatch) { batch.Requests[0].Initiator = yagomodel.Hash("bad") },
		},
	}
}

func crawlOrderImportRequestMetadataCases() []crawlOrderImportBatchCase {
	return []crawlOrderImportBatchCase{
		{
			name: "duplicate request identity",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Requests = append(batch.Requests, batch.Requests[0])
			},
		},
		{
			name: "too many request metadata fields",
			mutate: func(batch *CrawlOrderImportBatch) {
				batch.Requests[0].SourceMetadata = NewCrawlSourceMetadata(metadataOverflowValues())
			},
		},
	}
}

func metadataOverflowValues() map[string]string {
	values := make(map[string]string, MaximumCrawlOrderImportMetadata+1)
	for index := 0; index <= MaximumCrawlOrderImportMetadata; index++ {
		values["key"+string(rune('a'+index))] = "value"
	}
	return values
}

func TestCrawlOrderImportBatchAcceptsBoundarySchemesAndMetadata(t *testing.T) {
	for _, rawURL := range []string{
		"http://example.test/",
		"https://example.test/",
		"ftp://example.test/archive.zip",
		"smb://example.test/share/document.pdf",
	} {
		batch := validCrawlOrderImportBatch()
		batch.Requests[0].URL = rawURL
		batch.Requests[0].Mode = CrawlRequestModeSitemap
		batch.Requests[0].ReferrerURL = "https://referrer.example.test/page"
		batch.Requests[0].Initiator = yagomodel.Hash("AAAAAAAAAAAA")
		batch.Profile.SourceMetadata = NewCrawlSourceMetadata(
			map[string]string{"source": "fixture"},
		)
		batch.Requests[0].SourceMetadata = NewCrawlSourceMetadata(map[string]string{"depth": "1"})
		batch.BatchID, _ = batch.ComputeBatchID()
		if err := batch.Validate(); err != nil {
			t.Errorf("valid frontier URL %q rejected: %v", rawURL, err)
		}
	}
	if empty := NewCrawlSourceMetadata(nil); empty != nil {
		t.Fatal("empty source metadata was not omitted")
	}
	var absent *CrawlSourceMetadata
	if absent.Get("source") != "" {
		t.Fatal("nil source metadata did not return an empty value")
	}
	metadata := NewCrawlSourceMetadata(map[string]string{"source": "fixture"})
	if metadata.Get("source") != "fixture" || metadata.Get("missing") != "" {
		t.Fatal("source metadata lookup returned an incorrect value")
	}
}

func TestCrawlOrderImportBatchRejectsUnserializableTimestamp(t *testing.T) {
	batch := validCrawlOrderImportBatch()
	batch.Requests[0].AppDate = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	batch.BatchID = strings.Repeat("a", 64)
	if err := batch.Validate(); err == nil {
		t.Fatal("timestamp outside JSON's supported range was accepted")
	}
}

func validCrawlOrderImportBatch() CrawlOrderImportBatch {
	return CrawlOrderImportBatch{
		Version:  1,
		SourceID: "source-1",
		Profile: CrawlProfile{
			Handle:          "profile-1",
			Name:            "fixture",
			URLMustMatch:    MatchAll,
			MaxPagesPerHost: UnlimitedPagesPerHost,
		},
		Requests: []CrawlRequest{{
			URL:           "https://example.test/page",
			ProfileHandle: "profile-1",
		}},
	}
}
