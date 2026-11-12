package crawlrpc

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestCrawlRunTallyReadsOlderWireSnapshot(t *testing.T) {
	wire := protowire.AppendTag(nil, 1, protowire.VarintType)
	wire = protowire.AppendVarint(wire, 9)
	wire = protowire.AppendTag(wire, 2, protowire.VarintType)
	wire = protowire.AppendVarint(wire, 4)
	var tally CrawlRunTally
	if err := proto.Unmarshal(wire, &tally); err != nil {
		t.Fatalf("decode older tally: %v", err)
	}
	if tally.GetFetched() != 9 || tally.GetIndexed() != 4 || tally.GetDocumentCandidates() != 0 ||
		tally.GetNotIndexable() != 0 || tally.GetPendingIngest() != 0 ||
		len(tally.GetOutcomeCounts()) != 0 {
		t.Fatalf("older tally decoded as %+v", &tally)
	}
}
