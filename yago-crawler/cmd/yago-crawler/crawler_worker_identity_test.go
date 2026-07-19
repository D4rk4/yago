package main

import "testing"

func TestLoadCrawlerWorkerIdentityPrefix(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		identity, err := loadCrawlerWorkerIdentityPrefix(
			envFrom(map[string]string{EnvWorkerID: raw}),
		)
		if err != nil || identity != DefaultWorkerID {
			t.Fatalf("default worker identity = %q, err = %v", identity, err)
		}
	}
	identity, err := loadCrawlerWorkerIdentityPrefix(envFrom(map[string]string{
		EnvWorkerID: "  crawler-7  ",
	}))
	if err != nil || identity != "crawler-7" {
		t.Fatalf("configured worker identity = %q, err = %v", identity, err)
	}
	for _, raw := range []string{
		"crawler\n7",
		"crawler\u200b7",
		"crawler\u20287",
		"crawler\u20297",
		string([]byte{'c', 0xff}),
	} {
		if _, err := loadCrawlerWorkerIdentityPrefix(envFrom(map[string]string{
			EnvWorkerID: raw,
		})); err == nil {
			t.Errorf("worker identity %q loaded", raw)
		}
	}
}
