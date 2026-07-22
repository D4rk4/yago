package yagomodel

import (
	"math"
	"testing"
)

func TestYaCyVersionFloatMatchesJavaDoubleVocabulary(t *testing.T) {
	tests := []struct {
		raw  YaCyVersion
		want float64
	}{
		{raw: "\x00 1.5d \x1f", want: 1.5},
		{raw: "0x1.8p1F", want: 3},
		{raw: "Infinity", want: math.Inf(1)},
		{raw: "+Infinity", want: math.Inf(1)},
		{raw: "-Infinity", want: math.Inf(-1)},
		{raw: "1e400", want: math.Inf(1)},
	}
	for _, test := range tests {
		got, err := test.raw.Float()
		if err != nil {
			t.Fatalf("Float(%q): %v", test.raw, err)
		}
		if got != test.want {
			t.Fatalf("Float(%q) = %v, want %v", test.raw, got, test.want)
		}
	}
}

func TestYaCyVersionFloatMatchesJavaSpecialValues(t *testing.T) {
	for _, raw := range []YaCyVersion{"NaN", "+NaN", "-NaN"} {
		got, err := raw.Float()
		if err != nil {
			t.Fatalf("Float(%q): %v", raw, err)
		}
		if !math.IsNaN(got) {
			t.Fatalf("Float(%q) = %v", raw, got)
		}
	}
}

func TestYaCyVersionFloatRejectsGoOnlyVocabulary(t *testing.T) {
	for _, raw := range []YaCyVersion{
		"nan", "+infinity", "Inf", "1_0", "NaNf", "Infinityd", "f", "dev",
	} {
		if _, err := raw.Float(); err == nil {
			t.Fatalf("Float(%q) succeeded", raw)
		}
	}
}
