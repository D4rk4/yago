package pageindex

import "testing"

func TestCardinalValueBounds(t *testing.T) {
	if got := cardinalValue(-1, maxUint8); got != 0 {
		t.Fatalf("negative cardinal = %d", got)
	}
	if got := cardinalValue(maxUint8+1, maxUint8); got != maxUint8 {
		t.Fatalf("clamped cardinal = %d", got)
	}
}
