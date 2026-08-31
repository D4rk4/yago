package yagonode

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestReleaseTestShuffleSeedIsRecordedAndBounded(t *testing.T) {
	contents, err := os.ReadFile("../../../tools/test-shuffle-seed")
	if err != nil {
		t.Fatalf("read test shuffle seed: %v", err)
	}
	if err := validateReleaseTestShuffleSeed(string(contents)); err != nil {
		t.Fatal(err)
	}
	for _, refused := range []string{"", "on", "0", "-1", "9223372036854775808"} {
		if err := validateReleaseTestShuffleSeed(refused); err == nil {
			t.Fatalf("invalid test shuffle seed %q was admitted", refused)
		}
	}
}

func validateReleaseTestShuffleSeed(source string) error {
	value := strings.TrimSpace(source)
	seed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seed < 1 {
		return fmt.Errorf("invalid test shuffle seed %q", value)
	}
	return nil
}
