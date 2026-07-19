package yagocrawlcontract_test

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestParseProcessPagesPerSecond(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want uint32
	}{
		{raw: "0", want: 0},
		{raw: " 10 ", want: 10},
		{raw: "+120", want: 120},
		{raw: "0100000", want: 100_000},
	} {
		got, err := yagocrawlcontract.ParseProcessPagesPerSecond(test.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", test.raw, err)
		}
		if got != test.want {
			t.Fatalf("parse %q = %d, want %d", test.raw, got, test.want)
		}
	}
	for _, raw := range []string{"", "-1", "1000001", "4294967296", "one"} {
		if _, err := yagocrawlcontract.ParseProcessPagesPerSecond(raw); err == nil {
			t.Fatalf("invalid rate %q was accepted", raw)
		}
	}
}
