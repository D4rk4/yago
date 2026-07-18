package adminui

import "testing"

func TestFormatHistoryValueDoesNotLeaveBareDecimalPoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value float64
		want  string
	}{
		{value: 0.004, want: "0"},
		{value: -0.004, want: "0"},
		{value: 0.006, want: "0.01"},
		{value: 2.5, want: "2.5"},
		{value: 12.34, want: "12.34"},
	}
	for _, test := range tests {
		if got := formatHistoryValue(test.value); got != test.want {
			t.Fatalf("formatHistoryValue(%g) = %q, want %q", test.value, got, test.want)
		}
	}
}
