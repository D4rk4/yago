package main

import (
	"math"
	"testing"
)

func TestCrawlerByteSizeParsing(t *testing.T) {
	cases := []struct {
		raw      string
		fallback uint64
		want     uint64
	}{
		{fallback: 13, want: 13},
		{raw: "1B", want: 1},
		{raw: " 2 kb ", want: 2 << 10},
		{raw: "3mb", want: 3 << 20},
		{raw: "4GB", want: 4 << 30},
		{raw: "5tb", want: 5 << 40},
	}
	for _, test := range cases {
		got, err := envByteSize(
			func(string) string { return test.raw },
			"STORAGE",
			test.fallback,
		)
		if err != nil || got != test.want {
			t.Fatalf("parse %q = %d, %v, want %d", test.raw, got, err, test.want)
		}
	}
	for _, raw := range []string{
		"12",
		"XB",
		"not-a-numberB",
		"18446744073709551615TB",
		"9223372036854775808B",
	} {
		if _, err := envByteSize(func(string) string { return raw }, "STORAGE", 0); err == nil {
			t.Fatalf("invalid byte size %q accepted", raw)
		}
	}
	maximumBytes, err := envByteSize(
		func(string) string { return "9223372036854775807B" },
		"STORAGE",
		0,
	)
	if err != nil || maximumBytes != math.MaxInt64 {
		t.Fatalf("maximum bytes = %d, %v", maximumBytes, err)
	}
}

func TestServiceConfigLoadsCrawlerStoragePressure(t *testing.T) {
	defaults, err := LoadServiceConfig(envFrom(map[string]string{EnvNodeRPCAddr: "node:9091"}))
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if defaults.StorageReservedFreeBytes != DefaultStorageReservedFree ||
		defaults.StoragePressureHysteresisBytes != DefaultStoragePressureHysteresis {
		t.Fatalf("default storage policy = %+v", defaults)
	}
	overrides, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNodeRPCAddr:               "node:9091",
		EnvStorageReservedFree:       "2GB",
		EnvStoragePressureHysteresis: "64MB",
	}))
	if err != nil {
		t.Fatalf("load overrides: %v", err)
	}
	if overrides.StorageReservedFreeBytes != 2<<30 ||
		overrides.StoragePressureHysteresisBytes != 64<<20 {
		t.Fatalf("override storage policy = %+v", overrides)
	}
	for _, key := range []string{EnvStorageReservedFree, EnvStoragePressureHysteresis} {
		if _, err := LoadServiceConfig(envFrom(map[string]string{
			EnvNodeRPCAddr: "node:9091",
			key:            "invalid",
		})); err == nil {
			t.Fatalf("invalid %s accepted", key)
		}
	}
}
