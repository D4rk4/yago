package infrastructure

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func entryWithDocType(doctype byte) yacymodel.RWIEntry {
	return yacymodel.RWIEntry{Properties: map[string]string{
		yacymodel.ColDocType: yacymodel.Encode([]byte{doctype}),
	}}
}

func entryWithFlag(bit int) yacymodel.RWIEntry {
	flags := []byte{0, 0, 0, 0}
	flags[bit>>3] |= 1 << (bit % 8)
	return yacymodel.RWIEntry{Properties: map[string]string{
		yacymodel.ColFlags: yacymodel.Encode(flags),
	}}
}

func TestMatchesSiteHash(t *testing.T) {
	const urlHash = yacymodel.Hash("0123456789AB")
	if !matchesSiteHash(urlHash, "") {
		t.Fatal("empty site hash should match")
	}
	if !matchesSiteHash(urlHash, "6789AB") {
		t.Fatal("matching host hash should match")
	}
	if matchesSiteHash(urlHash, "000000") {
		t.Fatal("non-matching host hash should not match")
	}
}

func TestMatchesContentDomainStrict(t *testing.T) {
	if !matchesContentDomain(entryWithDocType(yacymodel.DocTypeImage), "image", true) {
		t.Fatal("image doctype should match strict image")
	}
	if matchesContentDomain(entryWithDocType(yacymodel.DocTypeAudio), "image", true) {
		t.Fatal("audio doctype should not match strict image")
	}
	if !matchesContentDomain(entryWithDocType(yacymodel.DocTypeMovie), "video", true) {
		t.Fatal("movie doctype should match strict video")
	}
}

func TestMatchesContentDomainNonStrict(t *testing.T) {
	if !matchesContentDomain(entryWithFlag(yacymodel.RWIFlagHasAudio), "audio", false) {
		t.Fatal("audio flag should match non-strict audio")
	}
	if matchesContentDomain(entryWithFlag(yacymodel.RWIFlagHasImage), "audio", false) {
		t.Fatal("image flag should not match non-strict audio")
	}
	if !matchesContentDomain(entryWithFlag(yacymodel.RWIFlagHasApp), "app", false) {
		t.Fatal("app flag should match app")
	}
}

func TestMatchesContentDomainPassthrough(t *testing.T) {
	entry := entryWithDocType(yacymodel.DocTypeImage)
	if !matchesContentDomain(entry, "", false) {
		t.Fatal("empty domain should pass through")
	}
	if !matchesContentDomain(entry, "text", true) {
		t.Fatal("text domain should pass through")
	}
}

func TestMatchesConstraint(t *testing.T) {
	entry := entryWithFlag(yacymodel.RWIFlagHasImage)

	if !matchesConstraint(entry, "") {
		t.Fatal("empty constraint should match")
	}

	allOn := yacymodel.Encode([]byte{0xff, 0xff, 0xff, 0xff})
	if !matchesConstraint(entry, allOn) {
		t.Fatal("all-on constraint is a no-op and should match")
	}

	require := []byte{0, 0, 0, 0}
	require[yacymodel.RWIFlagHasImage>>3] |= 1 << (yacymodel.RWIFlagHasImage % 8)
	if !matchesConstraint(entry, yacymodel.Encode(require)) {
		t.Fatal("constraint requiring a present flag should match")
	}

	other := []byte{0, 0, 0, 0}
	other[yacymodel.RWIFlagHasVideo>>3] |= 1 << (yacymodel.RWIFlagHasVideo % 8)
	if matchesConstraint(entry, yacymodel.Encode(other)) {
		t.Fatal("constraint requiring an absent flag should not match")
	}
}
