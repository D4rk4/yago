package recrawlfrontier

import (
	"bytes"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestDueKeyRoundTripsTimeAndHash(t *testing.T) {
	when := time.Date(2026, 7, 4, 9, 30, 15, 42, time.UTC)
	key := dueKey(when, "abcde")

	got, err := nextDueFromKey(key)
	if err != nil {
		t.Fatalf("nextDueFromKey: %v", err)
	}
	if !got.Equal(when) {
		t.Fatalf("time = %v, want %v", got, when)
	}
	hash, err := hashFromDueKey(key)
	if err != nil {
		t.Fatalf("hashFromDueKey: %v", err)
	}
	if hash != "abcde" {
		t.Fatalf("hash = %q, want abcde", hash)
	}
}

func TestDueKeySortsChronologically(t *testing.T) {
	early := dueKey(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "h")
	late := dueKey(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "h")
	if bytes.Compare(early, late) >= 0 {
		t.Fatalf("earlier due key must sort before later due key")
	}
}

func TestDueKeyClampsNegativeTime(t *testing.T) {
	key := dueKey(time.Unix(0, -5), "h")
	got, err := nextDueFromKey(key)
	if err != nil {
		t.Fatalf("nextDueFromKey: %v", err)
	}
	if got.UnixNano() != 0 {
		t.Fatalf("negative time not clamped: %v", got)
	}
}

func TestNextDueFromKeyRejectsMalformed(t *testing.T) {
	if _, err := nextDueFromKey(vault.Key("no-separator")); err == nil {
		t.Fatal("expected error for key without separator")
	}
	malformed := append([]byte("notanumber"), dueKeySeparator)
	malformed = append(malformed, "hash"...)
	if _, err := nextDueFromKey(malformed); err == nil {
		t.Fatal("expected error for non-numeric time prefix")
	}
}

func TestHashFromDueKeyRejectsMalformed(t *testing.T) {
	if _, err := hashFromDueKey(vault.Key("no-separator")); err == nil {
		t.Fatal("expected error for key without separator")
	}
}

func TestPresenceCodecRoundTrips(t *testing.T) {
	codec := presenceCodec{}
	raw, err := codec.Encode(struct{}{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := codec.Decode(raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestRecordCodecRoundTripsAndRejectsGarbage(t *testing.T) {
	codec := recordCodec{}
	record := scheduleRecord{
		URL:           "https://x.example/",
		ProfileHandle: "handle",
		Interval:      time.Hour,
		NextDueAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}
	raw, err := codec.Encode(record)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := codec.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.URL != record.URL ||
		back.ProfileHandle != record.ProfileHandle ||
		back.Interval != record.Interval ||
		!back.NextDueAt.Equal(record.NextDueAt) {
		t.Fatalf("round trip = %+v, want %+v", back, record)
	}
	if _, err := codec.Decode([]byte("{not json")); err == nil {
		t.Fatal("expected error decoding garbage")
	}
}
