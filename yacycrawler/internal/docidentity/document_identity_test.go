package docidentity_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/docidentity"
)

func TestDocumentIDIsStableAndDistinct(t *testing.T) {
	a := docidentity.DocumentID("https://example.com/")
	b := docidentity.DocumentID("https://example.com/")
	c := docidentity.DocumentID("https://example.com/other")
	if a != b {
		t.Error("same canonical URL should hash identically")
	}
	if a == c {
		t.Error("different canonical URLs should not collide")
	}
}
