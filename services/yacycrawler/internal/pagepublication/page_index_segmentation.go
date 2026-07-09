package pagepublication

import (
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func segmentCrawledPageIndex(
	index yacycrawlcontract.CrawledPageIndex,
) []yacycrawlcontract.CrawledPageIndexSegment {
	var segments []yacycrawlcontract.CrawledPageIndexSegment
	if len(index.Metadata) > 0 {
		segments = append(segments, yacycrawlcontract.CrawledPageIndexSegment{
			CanonicalURL: index.CanonicalURL,
			Metadata:     index.Metadata,
		})
	}
	for start := 0; start < len(index.Postings); start += yacycrawlcontract.PostingsPerSegmentLimit {
		end := min(start+yacycrawlcontract.PostingsPerSegmentLimit, len(index.Postings))
		segments = append(segments, yacycrawlcontract.CrawledPageIndexSegment{
			CanonicalURL: index.CanonicalURL,
			Postings:     index.Postings[start:end],
		})
	}
	return segments
}
