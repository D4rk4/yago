package yagomodel

import (
	"errors"
	"testing"
)

func TestNormalizeRWIPropertyNumericForms(t *testing.T) {
	cases := map[string]string{
		"":                               FormatRWICardinal(0),
		"+":                              FormatRWICardinal(0),
		"abc":                            FormatRWICardinal(0),
		"  +42x":                         FormatRWICardinal(42),
		"-1":                             FormatRWICardinal(255),
		"999999999999999999999999999999": FormatRWICardinal(0),
	}
	for raw, want := range cases {
		got, err := normalizeRWIProperty(ColHitCount, raw)
		if err != nil {
			t.Fatalf("normalizeRWIProperty(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalizeRWIProperty(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNormalizeRWIPropertiesReturnsFirstInvalidValue(t *testing.T) {
	if _, err := normalizeRWIProperties(map[string]string{ColFlags: "==="}); !errors.Is(
		err,
		errInvalidRWIProperty,
	) {
		t.Fatalf("normalizeRWIProperties err = %v, want errInvalidRWIProperty", err)
	}
}

func TestNormalizeRWIPropertyClampsTypedValues(t *testing.T) {
	docType, err := normalizeRWIProperty(ColDocType, "300")
	if err != nil {
		t.Fatal(err)
	}
	if docType != "44" {
		t.Fatalf("doctype = %q", docType)
	}

	lang, err := normalizeRWIProperty(ColLanguage, "eng")
	if err != nil {
		t.Fatal(err)
	}
	if lang != "en" {
		t.Fatalf("language = %q", lang)
	}

	flags, err := normalizeRWIProperty(ColFlags, Encode([]byte{1, 2, 3, 4, 5}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Decode(flags)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != rwiByteFlagLength {
		t.Fatalf("flags length = %d", len(raw))
	}

	reserve, err := normalizeRWIProperty(ColReserve, "257")
	if err != nil {
		t.Fatal(err)
	}
	if reserve != FormatRWICardinal(1) {
		t.Fatalf("reserve = %q", reserve)
	}

	short, err := normalizeRWIProperty(ColLanguage, "e")
	if err != nil {
		t.Fatal(err)
	}
	if short != "e" {
		t.Fatalf("short language = %q", short)
	}
}

func TestNormalizeRWIPropertyRejectsBadFlags(t *testing.T) {
	if _, err := normalizeRWIProperty(ColFlags, "==="); !errors.Is(err, errInvalidRWIProperty) {
		t.Fatalf("bad flags err = %v, want errInvalidRWIProperty", err)
	}
}

func TestValidateRWIPropertyRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		ColURLHash:       "short",
		ColLanguage:      "eng",
		ColFlags:         "===",
		ColDocType:       "999",
		ColHitCount:      "notnum",
		ColTextWordCount: "notnum",
	}
	for key, value := range cases {
		if err := validateRWIProperty(key, value); !errors.Is(err, errInvalidRWIProperty) {
			t.Fatalf(
				"validateRWIProperty(%q, %q) = %v, want errInvalidRWIProperty",
				key,
				value,
				err,
			)
		}
	}
}

func TestValidateRWIPropertiesReturnsFirstInvalidValue(t *testing.T) {
	err := validateRWIProperties(map[string]string{
		ColURLHash:  "MNOPQRSTUVWX",
		ColLanguage: "eng",
	})
	if !errors.Is(err, errInvalidRWIProperty) {
		t.Fatalf("validateRWIProperties err = %v, want errInvalidRWIProperty", err)
	}
}

func TestFixedWidthUnsignedKeepsWideValues(t *testing.T) {
	got := fixedWidthUnsigned(rwiDecimalPrefix{magnitude: 1 << 40}, 8)
	if got != 1<<40 {
		t.Fatalf("wide value = %d", got)
	}
}
