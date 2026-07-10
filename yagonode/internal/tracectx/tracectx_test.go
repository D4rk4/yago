package tracectx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseAcceptsValidTraceparent(t *testing.T) {
	trace, ok := Parse("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if !ok || trace.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" ||
		trace.SpanID != "00f067aa0ba902b7" || !trace.Sampled {
		t.Fatalf("trace = %+v ok=%v", trace, ok)
	}
	if trace, ok := Parse("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"); !ok ||
		trace.Sampled {
		t.Fatalf("unsampled flag misread: %+v", trace)
	}
}

func TestParseRejectsMalformedHeaders(t *testing.T) {
	invalid := []string{
		"",
		"garbage",
		"01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-" + strings.Repeat("0", 32) + "-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-" + strings.Repeat("0", 16) + "-01",
		"00-SHORT-00f067aa0ba902b7-01",
	}
	for _, header := range invalid {
		if _, ok := Parse(header); ok {
			t.Fatalf("accepted %q", header)
		}
	}
}

func TestNewAndChildKeepTheTrace(t *testing.T) {
	trace := New()
	if len(trace.TraceID) != 32 || len(trace.SpanID) != 16 {
		t.Fatalf("trace = %+v", trace)
	}
	child := trace.Child()
	if child.TraceID != trace.TraceID || child.SpanID == trace.SpanID {
		t.Fatalf("child = %+v from %+v", child, trace)
	}
	if reparsed, ok := Parse(trace.Header()); !ok || reparsed != trace {
		t.Fatalf("header round-trip: %+v vs %+v", reparsed, trace)
	}
}

func TestContextRoundTrip(t *testing.T) {
	trace := New()
	ctx := WithContext(context.Background(), trace)
	got, ok := FromContext(ctx)
	if !ok || got != trace {
		t.Fatalf("context round-trip: %+v ok=%v", got, ok)
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("empty context must carry no trace")
	}
}

func TestSamplingIsAMinorityShare(t *testing.T) {
	sampled := 0
	for range 2048 {
		if New().Sampled {
			sampled++
		}
	}
	if sampled == 0 || sampled > 2048/8 {
		t.Fatalf("sampled = %d of 2048, want a small non-zero share", sampled)
	}
}

func TestHeaderRendersSampledFlag(t *testing.T) {
	sampled := Trace{TraceID: "abc", SpanID: "def", Sampled: true}.Header()
	if sampled != "00-abc-def-01" {
		t.Fatalf("sampled header = %q, want the 01 flag", sampled)
	}
	unsampled := Trace{TraceID: "abc", SpanID: "def"}.Header()
	if unsampled != "00-abc-def-00" {
		t.Fatalf("unsampled header = %q, want the 00 flag", unsampled)
	}
}

func TestRandomHexFallsBackWhenEntropyFails(t *testing.T) {
	saved := randRead
	t.Cleanup(func() { randRead = saved })
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }

	if got, want := randomHex(8), strings.Repeat("0", 8*2-1)+"1"; got != want {
		t.Fatalf("randomHex fallback = %q, want %q", got, want)
	}
}
