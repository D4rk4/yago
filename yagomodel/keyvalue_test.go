package yagomodel

import "testing"

func TestParseMessage(t *testing.T) {
	msg, err := ParseMessage("version=1.0\r\nuptime=42\n\nresult=ok\n")
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg["version"] != "1.0" {
		t.Errorf("version = %q", msg["version"])
	}
	if msg["uptime"] != "42" {
		t.Errorf("uptime = %q", msg["uptime"])
	}
	if msg["result"] != "ok" {
		t.Errorf("result = %q", msg["result"])
	}
	if len(msg) != 3 {
		t.Errorf("expected 3 fields, got %d", len(msg))
	}
}

func TestParseMessageIgnoresMalformedLines(t *testing.T) {
	msg, err := ParseMessage("noeq\n=novalue\nok=yes\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(msg) != 1 || msg["ok"] != "yes" {
		t.Errorf("message = %v", msg)
	}
}

func TestParseMessageLastWins(t *testing.T) {
	msg, err := ParseMessage("k=first\nk=second\n")
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg["k"] != "second" {
		t.Errorf("k = %q, want second", msg["k"])
	}
}

func TestParseMessageUsesYaCyTableRules(t *testing.T) {
	msg, err := ParseMessage("# ignored\n spaced = value \nkey\\=part=line\\nnext\\\\tail\n")
	if err != nil {
		t.Fatal(err)
	}
	if msg["spaced"] != "value" {
		t.Errorf("spaced = %q", msg["spaced"])
	}
	if msg["key=part"] != "line\nnext\\tail" {
		t.Errorf("escaped = %q", msg["key=part"])
	}
}

func TestMessageEncodeDeterministic(t *testing.T) {
	msg := Message{"b": "2", "a": "1", "c": "3"}
	want := "a=1\nb=2\nc=3\n"
	if got := msg.Encode(); got != want {
		t.Errorf("Encode() = %q, want %q", got, want)
	}
}

func TestMessageRoundTrip(t *testing.T) {
	msg := Message{"version": "1.0", "result": "ok"}
	got, err := ParseMessage(msg.Encode())
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if got["version"] != "1.0" || got["result"] != "ok" {
		t.Errorf("round trip = %v", got)
	}
}
