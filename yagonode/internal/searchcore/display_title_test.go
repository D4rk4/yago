package searchcore

import "testing"

func TestDisplayTitle(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "ordinary title", input: "A title", want: "A title"},
		{name: "array title", input: `["A title", "Alternate"]`, want: "A title"},
		{name: "skip empty array values", input: `["", "A title"]`, want: "A title"},
		{name: "empty array", input: `[]`, want: ""},
		{name: "non-string array", input: `[7]`, want: ""},
		{name: "mixed array", input: `[7, "A title"]`, want: "A title"},
		{name: "malformed array", input: `["A title"`, want: `["A title"`},
		{name: "all blank values", input: `["", "  "]`, want: ""},
		{name: "json object", input: `{"title":"A title"}`, want: `{"title":"A title"}`},
		{name: "json null", input: `null`, want: `null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := DisplayTitle(test.input); got != test.want {
				t.Fatalf("DisplayTitle(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
