package infrastructure

import (
	"strconv"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func singlePostingHash(t *testing.T, postings []yacymodel.RWIPosting) yacymodel.Hash {
	t.Helper()

	if len(postings) != 1 {
		t.Fatalf("postings = %d, want 1", len(postings))
	}
	hash, err := postings[0].URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}
	return hash
}

func rwiPostingWithDocType(doctype byte) func(yacymodel.RWIPosting) yacymodel.RWIPosting {
	return func(entry yacymodel.RWIPosting) yacymodel.RWIPosting {
		entry.Properties[yacymodel.ColDocType] = strconv.FormatUint(uint64(doctype), 10)
		return entry
	}
}

func rwiPostingWithFlag(bit int) func(yacymodel.RWIPosting) yacymodel.RWIPosting {
	return func(entry yacymodel.RWIPosting) yacymodel.RWIPosting {
		entry.Properties[yacymodel.ColFlags] = rwiConstraintWithFlag(bit)
		return entry
	}
}

func rwiConstraintWithFlag(bit int) string {
	flags := []byte{0, 0, 0, 0}
	flags[bit>>3] |= 1 << (bit % 8)
	return yacymodel.Encode(flags)
}
