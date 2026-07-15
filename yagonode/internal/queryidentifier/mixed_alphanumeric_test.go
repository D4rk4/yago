package queryidentifier

import "testing"

func TestMixedAlphanumeric(t *testing.T) {
	tests := []struct {
		term string
		want bool
	}{
		{term: "ZX900Q", want: true},
		{term: " М2 ", want: true},
		{term: "model", want: false},
		{term: "900", want: false},
		{term: "ZX-900Q", want: false},
		{term: "", want: false},
	}
	for _, test := range tests {
		if got := MixedAlphanumeric(test.term); got != test.want {
			t.Errorf("MixedAlphanumeric(%q) = %v, want %v", test.term, got, test.want)
		}
	}
}
