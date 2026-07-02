package peernews

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

func TestNewsRecordWireFormRoundTrip(t *testing.T) {
	record := Record{
		Originator:  yacymodel.WordHash("myseed"),
		Created:     time.Date(2018, 6, 28, 14, 7, 13, 0, time.UTC),
		Received:    time.Date(2018, 6, 28, 15, 0, 0, 0, time.UTC),
		Category:    "crwlstrt",
		Distributed: 7,
		Attributes:  map[string]string{"startURL": "http://example.test/"},
	}

	parsed, err := parseRecord(record.WireForm(), time.Time{})
	if err != nil {
		t.Fatalf("parseRecord: %v", err)
	}
	if parsed.ID() != record.ID() ||
		parsed.Category != record.Category ||
		parsed.Distributed != record.Distributed ||
		!parsed.Received.Equal(record.Received) ||
		parsed.Attributes["startURL"] != record.Attributes["startURL"] {
		t.Fatalf("parsed = %#v, want %#v", parsed, record)
	}
}

func TestNewsRecordWireFormOmitsUnsetReceived(t *testing.T) {
	record := Record{
		Originator: yacymodel.WordHash("myseed"),
		Created:    time.Date(2018, 6, 28, 14, 7, 13, 0, time.UTC),
		Category:   "TestCat",
	}

	parsed, err := parseRecord(record.WireForm(), time.Time{})
	if err != nil {
		t.Fatalf("parseRecord: %v", err)
	}
	if !parsed.Received.IsZero() {
		t.Fatalf("received = %v, want unset", parsed.Received)
	}
}

func TestParseRecordAcceptsJavaMapForm(t *testing.T) {
	hash := yacymodel.WordHash("myseed").String()
	wire := "{text=message 1, cat=TestCat, cre=20180628140713, dis=2, ori=" + hash + "}"

	now := func() time.Time { return time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC) }
	record, err := ParseRecord(wire, now)
	if err != nil {
		t.Fatalf("ParseRecord: %v", err)
	}
	if record.Category != "TestCat" ||
		record.Distributed != 2 ||
		record.Attributes["text"] != "message 1" {
		t.Fatalf("record = %#v", record)
	}
	if record.ID() != "20180628140713"+hash {
		t.Fatalf("id = %q", record.ID())
	}
	if !record.Received.Equal(now()) {
		t.Fatalf("received = %v, want arrival time %v", record.Received, now())
	}
}

func TestParseRecordDefaultsBadCreationToFallback(t *testing.T) {
	hash := yacymodel.WordHash("myseed").String()
	now := func() time.Time { return time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC) }

	record, err := ParseRecord("{cre=garbage,ori="+hash+",malformed,=orphan}", now)
	if err != nil {
		t.Fatalf("ParseRecord: %v", err)
	}
	if !record.Created.Equal(now()) {
		t.Fatalf("created = %v, want fallback %v", record.Created, now())
	}
}

func TestParseRecordRejectsBadFields(t *testing.T) {
	hash := yacymodel.WordHash("myseed").String()
	now := func() time.Time { return time.Unix(100, 0) }

	for _, wire := range []string{
		"{cat=TestCat}",
		"{ori=" + hash + ",cat=much-too-long}",
		"{ori=" + hash + ",dis=many}",
	} {
		if _, err := ParseRecord(wire, now); err == nil {
			t.Errorf("ParseRecord(%q) did not fail", wire)
		}
	}
}
