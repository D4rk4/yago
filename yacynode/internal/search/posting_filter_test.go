package search

import (
	"context"
	"strconv"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func withDocType(doctype byte) func(yacymodel.RWIPosting) yacymodel.RWIPosting {
	return func(entry yacymodel.RWIPosting) yacymodel.RWIPosting {
		entry.Properties[yacymodel.ColDocType] = strconv.FormatUint(uint64(doctype), 10)

		return entry
	}
}

func withFlag(bit int) func(yacymodel.RWIPosting) yacymodel.RWIPosting {
	return func(entry yacymodel.RWIPosting) yacymodel.RWIPosting {
		entry.Properties[yacymodel.ColFlags] = constraintWithFlag(bit)

		return entry
	}
}

func constraintWithFlag(bit int) string {
	flags := []byte{0, 0, 0, 0}
	flags[bit>>3] |= 1 << (bit % 8)

	return yacymodel.Encode(flags)
}

func emptyPosting() yacymodel.RWIPosting {
	return yacymodel.RWIPosting{Properties: map[string]string{}}
}

func TestMatchesContentDomainStrict(t *testing.T) {
	ctx := context.Background()
	if !matchesContentDomain(
		ctx,
		withDocType(yacymodel.DocTypeImage)(emptyPosting()),
		"image",
		true,
	) {
		t.Fatal("image doctype should match strict image")
	}
	if matchesContentDomain(
		ctx,
		withDocType(yacymodel.DocTypeAudio)(emptyPosting()),
		"image",
		true,
	) {
		t.Fatal("audio doctype should not match strict image")
	}
	if !matchesContentDomain(
		ctx,
		withDocType(yacymodel.DocTypeMovie)(emptyPosting()),
		"video",
		true,
	) {
		t.Fatal("movie doctype should match strict video")
	}
}

func TestMatchesContentDomainNonStrict(t *testing.T) {
	ctx := context.Background()
	if !matchesContentDomain(
		ctx,
		withFlag(yacymodel.RWIFlagHasAudio)(emptyPosting()),
		"audio",
		false,
	) {
		t.Fatal("audio flag should match non-strict audio")
	}
	if matchesContentDomain(
		ctx,
		withFlag(yacymodel.RWIFlagHasImage)(emptyPosting()),
		"audio",
		false,
	) {
		t.Fatal("image flag should not match non-strict audio")
	}
	if !matchesContentDomain(
		ctx,
		withFlag(yacymodel.RWIFlagHasVideo)(emptyPosting()),
		"video",
		false,
	) {
		t.Fatal("video flag should match non-strict video")
	}
	if !matchesContentDomain(ctx, withFlag(yacymodel.RWIFlagHasApp)(emptyPosting()), "app", false) {
		t.Fatal("app flag should match app")
	}
}

func TestMatchesContentDomainPassthrough(t *testing.T) {
	ctx := context.Background()
	entry := withDocType(yacymodel.DocTypeImage)(emptyPosting())
	if !matchesContentDomain(ctx, entry, "", false) {
		t.Fatal("empty domain should pass through")
	}
	if !matchesContentDomain(ctx, entry, "text", true) {
		t.Fatal("text domain should pass through")
	}
}

func TestMatchesSiteHash(t *testing.T) {
	const urlHash = yacymodel.URLHash("0123456789AB")
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

func TestMatchesConstraint(t *testing.T) {
	ctx := context.Background()
	entry := withFlag(yacymodel.RWIFlagHasImage)(emptyPosting())

	if !matchesConstraint(ctx, entry, "") {
		t.Fatal("empty constraint should match")
	}
	allOn := yacymodel.Encode([]byte{0xff, 0xff, 0xff, 0xff})
	if !matchesConstraint(ctx, entry, allOn) {
		t.Fatal("all-on constraint is a no-op and should match")
	}
	if !matchesConstraint(ctx, entry, constraintWithFlag(yacymodel.RWIFlagHasImage)) {
		t.Fatal("constraint requiring a present flag should match")
	}
	if matchesConstraint(ctx, entry, constraintWithFlag(yacymodel.RWIFlagHasVideo)) {
		t.Fatal("constraint requiring an absent flag should not match")
	}
}

func TestHashSet(t *testing.T) {
	if hashSet(nil) != nil {
		t.Fatal("nil input should return nil")
	}
	first, second := hashFor("url-a"), hashFor("url-b")
	set := hashSet([]yacymodel.Hash{first, second})
	if _, ok := set[first]; !ok {
		t.Fatal("first hash missing")
	}
	if _, ok := set[second]; !ok {
		t.Fatal("second hash missing")
	}
}

func TestSearchFiltersByLanguageAndDistanceAndURL(t *testing.T) {
	word := hashFor("w1")
	english := postingEntry(word, "u1", 1, 1)
	english.Properties[yacymodel.ColLanguage] = "en"
	german := postingEntry(word, "u2", 1, 1)
	german.Properties[yacymodel.ColLanguage] = "de"
	far := postingEntry(word, "u3", 9, 1)
	far.Properties[yacymodel.ColLanguage] = "en"

	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {english, german, far},
	}}
	s := searcher{
		index:           index,
		urls:            fakeDirectory{rows: urlRows("u1", "u2", "u3")},
		postingsPerWord: 100,
	}

	result, err := s.Search(context.Background(), searchQuery{
		Words:         []yacymodel.Hash{word},
		MaxDistance:   5,
		searchFilters: searchFilters{Modifier: "/language/en"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Resources) != 1 ||
		result.Resources[0].Properties[yacymodel.URLMetaHash] != string(hashFor("u1")) {
		t.Fatalf("Resources = %v, want only u1", result.Resources)
	}
}
