package tracectx

import (
	"context"
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
