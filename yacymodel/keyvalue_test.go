package yacymodel

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

func TestParseMessageRejectsMalformedLine(t *testing.T) {
	for _, input := range []string{"noeq\n", "=novalue\n"} {
		if _, err := ParseMessage(input); err == nil {
			t.Errorf("ParseMessage(%q) error = nil, want error", input)
		}
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
