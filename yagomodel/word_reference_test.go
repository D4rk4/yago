package yagomodel

import (
	"errors"
	"strings"
	"testing"
)

func TestWordReferencePropertyFormRoundTrip(t *testing.T) {
	posting := RWIPosting{Properties: map[string]string{
		ColURLHash:      "MNOPQRSTUVWX",
		ColHitCount:     "7",
		ColTextPosition: "23",
	}}
	propertyForm := WordReferencePropertyForm(posting)
	reference, err := ParseWordReference(propertyForm)
	if err != nil {
		t.Fatalf("ParseWordReference: %v", err)
	}
	if hash := reference.URLHash(); hash != "MNOPQRSTUVWX" {
		t.Fatalf("URLHash = %q", hash)
	}
	if reference.Properties[ColHitCount] != "7" ||
		reference.Properties[ColTextPosition] != "23" ||
		reference.Properties[ColLanguage] != "en" {
		t.Fatalf("properties = %#v", reference.Properties)
	}
}

func TestParseWordReferenceRequiresFixedColumnShape(t *testing.T) {
	valid := WordReferencePropertyForm(RWIPosting{Properties: map[string]string{
		ColURLHash: "MNOPQRSTUVWX",
	}})
	cases := []string{
		"",
		"{h=MNOPQRSTUVWX}",
		valid[:len(valid)-1] + ",b=0}",
		strings.Replace(valid, "h=", "h", 1),
		strings.Replace(valid, "h=", "x=", 1),
	}
	for _, propertyForm := range cases {
		if _, err := ParseWordReference(propertyForm); !errors.Is(err, ErrBadWordReference) {
			t.Fatalf("ParseWordReference(%q) error = %v", propertyForm, err)
		}
	}
}

func TestParseWordReferenceRejectsInvalidFixedValues(t *testing.T) {
	valid := WordReferencePropertyForm(RWIPosting{Properties: map[string]string{
		ColURLHash: "short",
	}})
	for _, propertyForm := range []string{
		valid,
		strings.Replace(valid, "z=AAAAAA", "z====", 1),
	} {
		if _, err := ParseWordReference(propertyForm); !errors.Is(err, ErrBadWordReference) {
			t.Fatalf("ParseWordReference(%q) error = %v", propertyForm, err)
		}
	}
}
