package yagocrawlcontract

import "github.com/D4rk4/yago/yagomodel"

const (
	MaximumIngestBatchBytes      = (4 << 20) - (64 << 10)
	MaximumIngestMessageBytes    = MaximumIngestBatchBytes + (64 << 10)
	MaximumIngestPostings        = 8192
	MaximumDocumentTextBytes     = 1 << 20
	MaximumDocumentWords         = 65536
	MaximumDocumentTitleBytes    = 4096
	MaximumDocumentHeadingBytes  = 2048
	MaximumDocumentHeadings      = 256
	MaximumDocumentOutlinks      = 4096
	MaximumDocumentAnchors       = 1024
	MaximumDocumentImages        = 32
	MaximumDocumentMetadata      = 32
	MaximumDocumentMetadataBytes = 8192
	MaximumCrawlURLBytes         = yagomodel.MaximumURLIdentityBytes
	MaximumProfileHandleBytes    = 256
	MaximumProvenanceBytes       = 4096
	MaximumMetadataRows          = 16
	MaximumPropertyEntries       = 64
)
