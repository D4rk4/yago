package yagoproto

import "testing"

func TestParseJavaSignedDecimalInt32(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  int
		valid bool
	}{
		{name: "Arabic Indic", value: "١٢٣", want: 123, valid: true},
		{name: "fullwidth", value: "４２", want: 42, valid: true},
		{name: "positive boundary", value: "+2147483647", want: 2147483647, valid: true},
		{name: "negative boundary", value: "-2147483648", want: -2147483648, valid: true},
		{name: "negative zero", value: "-٠", want: 0, valid: true},
		{name: "positive overflow", value: "2147483648", valid: false},
		{name: "negative overflow", value: "-2147483649", valid: false},
		{name: "supplementary decimal digit", value: "𝟙", valid: false},
		{name: "empty", value: "", valid: false},
		{name: "sign only", value: "+", valid: false},
		{name: "Unicode minus", value: "−1", valid: false},
		{name: "Roman numeral", value: "Ⅳ", valid: false},
		{name: "whitespace", value: " 1", valid: false},
		{name: "invalid UTF-8", value: string([]byte{0xff}), valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := parseJavaSignedDecimalInt32(test.value)
			if got != test.want || valid != test.valid {
				t.Fatalf(
					"parseJavaSignedDecimalInt32(%q) = %d, %v; want %d, %v",
					test.value,
					got,
					valid,
					test.want,
					test.valid,
				)
			}
		})
	}
}
