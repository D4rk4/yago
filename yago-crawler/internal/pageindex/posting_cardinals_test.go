package pageindex

import "testing"

func TestComponentCardinalMatchesJavaByteValues(t *testing.T) {
	cases := map[int]string{
		0:   "0",
		1:   "1",
		100: "100",
		254: "254",
		255: "255",
		256: "0",
		257: "1",
		300: "44",
		510: "254",
	}
	for count, want := range cases {
		if got := componentCardinal(count); got != want {
			t.Errorf("componentCardinal(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestComponentCardinalNegativesClampToZero(t *testing.T) {
	for _, count := range []int{-1, -100, -256, -257} {
		if got := componentCardinal(count); got != "0" {
			t.Errorf("componentCardinal(%d) = %q, want 0", count, got)
		}
	}
}

func TestComponentCardinalInBandUnchanged(t *testing.T) {
	for _, count := range []int{254, 255} {
		if got := componentCardinal(count); got == "0" || got == "44" {
			t.Errorf("componentCardinal(%d) = %q, want %d", count, got, count)
		}
	}
}
