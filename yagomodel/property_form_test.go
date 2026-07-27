package yagomodel

import (
	"errors"
	"testing"
)

// A peer-supplied property body is the only place an RWI posting or a URL
// metadata row learns its field names. A token with no unescaped key would
// register an anonymous "" property, so the guard has to refuse both malformed
// shapes: a token with no separator at all, and a token whose separator sits at
// position zero.
func TestParsePropertyPairsRejectsMalformedTokens(t *testing.T) {
	for _, body := range []string{
		"badtoken",
		"=novalue",
		"h=MNOPQRSTUVWX,badtoken",
		"h=MNOPQRSTUVWX,=novalue",
	} {
		if _, err := parsePropertyPairs(body); !errors.Is(err, errBadPropertyForm) {
			t.Errorf("parsePropertyPairs(%q) = %v, want errBadPropertyForm", body, err)
		}
	}
}

// Position one is the accepting side of the same guard: every YaCy column name
// ("h", "c", "z") is a single byte, so refusing the smallest legal key would
// reject every real posting and metadata row on the wire.
func TestParsePropertyPairsAcceptsSingleBytePropertyKey(t *testing.T) {
	props, err := parsePropertyPairs("h=MNOPQRSTUVWX,c=1")
	if err != nil {
		t.Fatalf("parsePropertyPairs: %v", err)
	}
	if props[ColURLHash] != "MNOPQRSTUVWX" || props[ColHitCount] != "1" {
		t.Fatalf("props = %v", props)
	}
}

// Without the empty-key refusal an anonymous property is simply stored: the row
// then satisfies the non-empty check and a posting built from it carries no URL
// hash at all, which is a posting the vault can never address. Both peer row
// parsers must therefore surface the property-form refusal rather than fail
// later for an unrelated reason.
func TestPeerRowParsersSurfacePropertyFormRefusal(t *testing.T) {
	if _, err := ParseURIMetadataRow("{=novalue}"); !errors.Is(err, errBadPropertyForm) {
		t.Errorf("ParseURIMetadataRow = %v, want errBadPropertyForm", err)
	}
	if _, err := ParseRWIPosting("ABCDEFGHIJKL{=novalue}"); !errors.Is(err, errBadPropertyForm) {
		t.Errorf("ParseRWIPosting = %v, want errBadPropertyForm", err)
	}
}
