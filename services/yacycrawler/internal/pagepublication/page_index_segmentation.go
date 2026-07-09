package pagepublication

import (
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func segmentCrawledPageIndex(
	index yacycrawlcontract.CrawledPageIndex,
) []yacycrawlcontract.CrawledPageIndexMessage {
	var messages []yacycrawlcontract.CrawledPageIndexMessage
	if len(index.Metadata) > 0 {
		messages = append(messages, yacycrawlcontract.CrawledPageIndexMessage{
			CanonicalURL: index.CanonicalURL,
			Metadata:     index.Metadata,
		})
	}
	for start := 0; start < len(index.Postings); start += yacycrawlcontract.PostingsPerMessageLimit {
		end := min(start+yacycrawlcontract.PostingsPerMessageLimit, len(index.Postings))
		messages = append(messages, yacycrawlcontract.CrawledPageIndexMessage{
			CanonicalURL: index.CanonicalURL,
			Postings:     index.Postings[start:end],
		})
	}
	return messages
}
