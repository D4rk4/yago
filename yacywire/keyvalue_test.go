package yacywire

import "testing"

func TestParseMessage(t *testing.T) {
	msg := ParseMessage("version=1.0\r\nuptime=42\n\nnoeq\n=novalue\nresult=ok\n")
	if msg["version"] != "1.0" {
		t.Errorf("version = %q", msg["version"])
	}
	if msg["uptime"] != "42" {
		t.Errorf("uptime = %q", msg["uptime"])
	}
	if msg["result"] != "ok" {
		t.Errorf("result = %q", msg["result"])
	}
	if _, ok := msg["noeq"]; ok {
		t.Error("line without = should be ignored")
	}
	if len(msg) != 3 {
		t.Errorf("expected 3 fields, got %d", len(msg))
	}
}

func TestParseMessageLastWins(t *testing.T) {
	msg := ParseMessage("k=first\nk=second\n")
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
	got := ParseMessage(msg.Encode())
	if got["version"] != "1.0" || got["result"] != "ok" {
		t.Errorf("round trip = %v", got)
	}
}
