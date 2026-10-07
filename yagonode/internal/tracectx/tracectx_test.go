package tracectx

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
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

func TestParseSamplesTraceFlagBitZero(t *testing.T) {
	for _, test := range []struct {
		flags   string
		sampled bool
	}{
		{flags: "00"},
		{flags: "01", sampled: true},
		{flags: "02"},
		{flags: "03", sampled: true},
	} {
		header := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-" + test.flags
		trace, ok := Parse(header)
		if !ok || trace.Sampled != test.sampled {
			t.Fatalf("Parse(%q) = %+v, ok=%v; sampled=%v", header, trace, ok, test.sampled)
		}
		rendered, valid := Parse(trace.Header())
		if !valid || rendered.Sampled != test.sampled {
			t.Fatalf("trace header %q did not preserve sample state", trace.Header())
		}
	}
}

func TestStartServerSpanPreservesSampledBitFromParent(t *testing.T) {
	ctx, trace := StartServerSpan(
		context.Background(),
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-03",
	)
	if !trace.Sampled {
		t.Fatal("server child cleared the sampled bit from its parent")
	}
	stored, ok := FromContext(ctx)
	if !ok || stored != trace {
		t.Fatalf("stored child trace = %+v, ok=%v; want %+v", stored, ok, trace)
	}
	parsed, ok := Parse(trace.Header())
	if !ok || parsed != trace {
		t.Fatalf("server child header = %q parsed as %+v, ok=%v", trace.Header(), parsed, ok)
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

func TestStartServerSpanPreservesTraceAndCreatesNewSpan(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const parentID = "00f067aa0ba902b7"
	ctx, trace := StartServerSpan(
		context.Background(),
		"00-"+traceID+"-"+parentID+"-01",
	)
	if trace.TraceID != traceID || !trace.Sampled {
		t.Fatalf("server trace = %+v, want preserved trace ID and sampling", trace)
	}
	if trace.SpanID == parentID {
		t.Fatal("server span reused the caller's parent ID")
	}
	if stored, ok := FromContext(ctx); !ok || stored != trace {
		t.Fatalf("stored trace = %+v, ok=%v; want %+v", stored, ok, trace)
	}
	serverSpanID, ok := ServerSpanIDFromContext(ctx)
	if !ok || serverSpanID != trace.SpanID {
		t.Fatalf("server span ID = %q, ok=%v; want %q", serverSpanID, ok, trace.SpanID)
	}
}

func TestStartServerSpanCreatesFreshTraceForInvalidOrMissingParent(t *testing.T) {
	for _, header := range []string{"", "invalid-traceparent"} {
		ctx, trace := StartServerSpan(context.Background(), header)
		if len(trace.TraceID) != 32 || len(trace.SpanID) != 16 {
			t.Errorf("trace for header %q has invalid ID lengths: %+v", header, trace)
		}
		if _, err := hex.DecodeString(trace.TraceID + trace.SpanID); err != nil {
			t.Errorf("trace for header %q has invalid hexadecimal IDs: %v", header, err)
		}
		serverSpanID, ok := ServerSpanIDFromContext(ctx)
		if !ok || serverSpanID != trace.SpanID {
			t.Errorf("server span ID for header %q = %q, ok=%v", header, serverSpanID, ok)
		}
	}
}

func TestServerSpanIDDoesNotTrustTraceOnlyContext(t *testing.T) {
	clientTrace, ok := Parse("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if !ok {
		t.Fatal("synthetic traceparent was rejected")
	}
	ctx := WithContext(context.Background(), clientTrace)
	if serverSpanID, ok := ServerSpanIDFromContext(ctx); ok {
		t.Fatalf("trace-only context exposed server span ID %q", serverSpanID)
	}
}

func TestServerSpanAttributeOnlyEmitsTrustedServerSpan(t *testing.T) {
	clientTrace, ok := Parse("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if !ok {
		t.Fatal("synthetic traceparent was rejected")
	}
	trustedContext, serverTrace := StartServerSpan(
		context.Background(),
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	)
	for _, test := range []struct {
		name       string
		ctx        context.Context
		wantSpanID string
	}{
		{name: "absent", ctx: context.Background()},
		{name: "caller trace", ctx: WithContext(context.Background(), clientTrace)},
		{name: "trusted server span", ctx: trustedContext, wantSpanID: serverTrace.SpanID},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			logger.LogAttrs(test.ctx, slog.LevelInfo, "event", ServerSpanAttribute(test.ctx))

			entry := map[string]any{}
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatalf("decode log record: %v", err)
			}
			spanID, present := entry["serverSpanId"].(string)
			if test.wantSpanID == "" {
				if present {
					t.Fatalf("untrusted context emitted server span ID %q", spanID)
				}

				return
			}
			if !present || spanID != test.wantSpanID {
				t.Fatalf(
					"serverSpanId = %q, present=%v; want %q",
					spanID,
					present,
					test.wantSpanID,
				)
			}
		})
	}
}

func TestCopyServerSpanRequiresTrustedSource(t *testing.T) {
	target := context.WithValue(context.Background(), copyServerSpanTargetKey{}, "target")
	clientTrace, ok := Parse("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if !ok {
		t.Fatal("synthetic traceparent was rejected")
	}
	for _, source := range []context.Context{
		context.Background(),
		WithContext(context.Background(), clientTrace),
	} {
		if copied := CopyServerSpan(target, source); copied != target {
			t.Fatal("source without a trusted server span changed target context")
		}
	}
}

func TestCopyServerSpanPreservesTargetContextAndCopiesOnlyServerID(t *testing.T) {
	sourceBase, cancelSource := context.WithCancel(
		context.WithValue(context.Background(), copyServerSpanSourceKey{}, "source"),
	)
	defer cancelSource()
	sourceDeadlineContext, cancelSourceDeadline := context.WithDeadline(
		sourceBase,
		time.Now().Add(2*time.Hour),
	)
	defer cancelSourceDeadline()
	source, trace := StartServerSpan(
		sourceDeadlineContext,
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	)
	targetBase, cancelTarget := context.WithDeadline(
		context.WithValue(context.Background(), copyServerSpanTargetKey{}, "target"),
		time.Now().Add(time.Hour),
	)
	defer cancelTarget()
	copied := CopyServerSpan(targetBase, source)

	serverSpanID, ok := ServerSpanIDFromContext(copied)
	if !ok || serverSpanID != trace.SpanID {
		t.Fatalf("copied server span ID = %q, ok=%v; want %q", serverSpanID, ok, trace.SpanID)
	}
	if _, ok := FromContext(copied); ok {
		t.Fatal("copy inherited the source trace context")
	}
	if copied.Value(copyServerSpanTargetKey{}) != "target" ||
		copied.Value(copyServerSpanSourceKey{}) != nil {
		t.Fatal("copy did not preserve only target values")
	}
	targetDeadline, targetHasDeadline := targetBase.Deadline()
	copiedDeadline, copiedHasDeadline := copied.Deadline()
	if !targetHasDeadline || !copiedHasDeadline || !copiedDeadline.Equal(targetDeadline) {
		t.Fatal("copy did not preserve target deadline")
	}

	cancelSource()
	if copied.Err() != nil {
		t.Fatalf("source cancellation reached copied context: %v", copied.Err())
	}
	cancelTarget()
	if copied.Err() != context.Canceled {
		t.Fatalf("target cancellation = %v, want %v", copied.Err(), context.Canceled)
	}
}

type (
	copyServerSpanSourceKey struct{}
	copyServerSpanTargetKey struct{}
)

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
