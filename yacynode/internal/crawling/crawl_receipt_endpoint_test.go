package crawling

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestCrawlReceiptRejectsCrawl(t *testing.T) {
	req := yacyproto.CrawlReceiptRequest{
		NetworkName: "freeworld",
		Iam:         yacymodel.WordHash("caller"),
		YouAre:      yacymodel.WordHash("self"),
	}

	resp, err := crawlReceiptEndpoint{}.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Delay != 0 {
		t.Fatalf("Delay = %d, want 0 (no crawl accepted)", resp.Delay)
	}
}
