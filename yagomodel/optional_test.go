package yagomodel

import "testing"

func TestOptionalNoneAndPresent(t *testing.T) {
	none := None[string]()
	if none.Present() {
		t.Fatal("None should not be present")
	}
	if got, ok := none.Get(); ok || got != "" {
		t.Fatalf("None Get = %q, %v", got, ok)
	}

	some := Some("value")
	if !some.Present() {
		t.Fatal("Some should be present")
	}
	if got, ok := some.Get(); !ok || got != "value" {
		t.Fatalf("Some Get = %q, %v", got, ok)
	}
}
