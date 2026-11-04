package documentimport

import (
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	MaximumBatchRecords        = 256
	MaximumRequestBytes        = 64 << 20
	MaximumMigrationURLBytes   = yagocrawlcontract.MaximumCrawlURLBytes
	MaximumRecordTextBytes     = 32 << 20
	MaximumRecordMetadataBytes = 8 << 20
	MaximumRecordHeadingBytes  = yagocrawlcontract.MaximumDocumentHeadingBytes
	MaximumRecordHeadings      = 4096
	MaximumRecordOutlinks      = 131_072
	MaximumRecordInlinks       = 65_536
)
