package documentsearch

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func appearanceWithContentKind(kind byte) termAppearance {
	return termAppearance{contentKind: kind, contentKindKnown: true}
}

func appearanceWithFlag(bit int) termAppearance {
	flags, _ := yacymodel.DecodeBitfield(encodedFlag(bit))

	return termAppearance{appearanceFlags: flags}
}

func encodedFlag(bit int) string {
	flags := []byte{0, 0, 0, 0}
	flags[bit>>3] |= 1 << (bit % 8)

	return yacymodel.Encode(flags)
}

func TestMatchesContentKindStrict(t *testing.T) {
	ctx := context.Background()
	if !matchesContentKind(
		ctx,
		appearanceWithContentKind(yacymodel.DocTypeImage),
		imageContent,
		true,
	) {
		t.Fatal("image content kind should match strict image")
	}
	if matchesContentKind(
		ctx,
		appearanceWithContentKind(yacymodel.DocTypeAudio),
		imageContent,
		true,
	) {
		t.Fatal("audio content kind should not match strict image")
	}
	if !matchesContentKind(
		ctx,
		appearanceWithContentKind(yacymodel.DocTypeMovie),
		videoContent,
		true,
	) {
		t.Fatal("movie content kind should match strict video")
	}
}

func TestMatchesContentKindNonStrict(t *testing.T) {
	ctx := context.Background()
	if !matchesContentKind(
		ctx,
		appearanceWithFlag(yacymodel.RWIFlagHasAudio),
		audioContent,
		false,
	) {
		t.Fatal("audio flag should match non-strict audio")
	}
	if matchesContentKind(ctx, appearanceWithFlag(yacymodel.RWIFlagHasImage), audioContent, false) {
		t.Fatal("image flag should not match non-strict audio")
	}
	if !matchesContentKind(
		ctx,
		appearanceWithFlag(yacymodel.RWIFlagHasVideo),
		videoContent,
		false,
	) {
		t.Fatal("video flag should match non-strict video")
	}
	if !matchesContentKind(
		ctx,
		appearanceWithFlag(yacymodel.RWIFlagHasApp),
		applicationContent,
		false,
	) {
		t.Fatal("app flag should match app")
	}
}

func TestMatchesContentKindPassthrough(t *testing.T) {
	ctx := context.Background()
	appearance := appearanceWithContentKind(yacymodel.DocTypeImage)
	if !matchesContentKind(ctx, appearance, anyContent, false) {
		t.Fatal("any content kind should pass through")
	}
	if !matchesContentKind(ctx, appearance, anyContent, true) {
		t.Fatal("any content kind should pass through when strict")
	}
}

func TestMatchesSiteHost(t *testing.T) {
	ctx := context.Background()
	const location = yacymodel.URLHash("0123456789AB")
	if !matchesSiteHost(ctx, location, "") {
		t.Fatal("empty site hash should match")
	}
	if !matchesSiteHost(ctx, location, "6789AB") {
		t.Fatal("matching host hash should match")
	}
	if matchesSiteHost(ctx, location, "000000") {
		t.Fatal("non-matching host hash should not match")
	}
}

func decodedProperties(t *testing.T, encoded string) yacymodel.Bitfield {
	t.Helper()
	required, err := requiredProperties(encoded)
	if err != nil {
		t.Fatalf("requiredProperties: %v", err)
	}

	return required
}

func TestRequiredPropertiesNoOp(t *testing.T) {
	if decodedProperties(t, "") != nil {
		t.Fatal("empty required properties should be a no-op")
	}
	allOn := yacymodel.Encode([]byte{0xff, 0xff, 0xff, 0xff})
	if decodedProperties(t, allOn) != nil {
		t.Fatal("all-on required properties should be a no-op")
	}
}

func TestRequiredPropertiesRejectsMalformed(t *testing.T) {
	if _, err := requiredProperties("@@not-base64@@"); err == nil {
		t.Fatal("malformed required properties should fail")
	}
}

func TestMatchesRequiredProperties(t *testing.T) {
	ctx := context.Background()
	appearance := appearanceWithFlag(yacymodel.RWIFlagHasImage)

	if !matchesRequiredProperties(ctx, appearance, nil) {
		t.Fatal("no required properties should match")
	}
	if !matchesRequiredProperties(
		ctx,
		appearance,
		decodedProperties(t, encodedFlag(yacymodel.RWIFlagHasImage)),
	) {
		t.Fatal("required property present in appearance should match")
	}
	if matchesRequiredProperties(
		ctx,
		appearance,
		decodedProperties(t, encodedFlag(yacymodel.RWIFlagHasVideo)),
	) {
		t.Fatal("required property absent from appearance should not match")
	}
}

func TestDocumentSet(t *testing.T) {
	if documentSet(nil) != nil {
		t.Fatal("nil input should return nil")
	}
	first, second := hashFor("url-a"), hashFor("url-b")
	set := documentSet([]yacymodel.Hash{first, second})
	if _, ok := set[first]; !ok {
		t.Fatal("first identifier missing")
	}
	if _, ok := set[second]; !ok {
		t.Fatal("second identifier missing")
	}
}
