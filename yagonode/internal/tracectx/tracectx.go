// Package tracectx implements the W3C Trace Context header (traceparent) for
// the node's own request legs — enough to correlate one public search with
// the peer fan-out it caused and to attach exemplars to latency histograms
// (OPS-10). It is deliberately not an OpenTelemetry SDK: the scope is this
// node's logs and metrics, sampled, with zero new dependencies.
package tracectx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// Header is the W3C header name carrying the trace context.
const Header = "traceparent"

// Trace is one parsed or generated trace context: the trace ID ties every
// leg of a request together, the span ID names this hop.
type Trace struct {
	TraceID string
	SpanID  string
	Sampled bool
}

type contextKey struct{}

var traceparentPattern = regexp.MustCompile(
	`^00-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$`,
)

// Parse reads a traceparent header value; ok is false when the header is
// absent or malformed (the caller then starts a fresh trace).
func Parse(header string) (Trace, bool) {
	match := traceparentPattern.FindStringSubmatch(strings.TrimSpace(header))
	if match == nil || match[1] == strings.Repeat("0", 32) ||
		match[2] == strings.Repeat("0", 16) {
		return Trace{}, false
	}

	return Trace{
		TraceID: match[1],
		SpanID:  match[2],
		Sampled: match[3] == "01",
	}, true
}

// New starts a fresh trace. Sampling keys off the low byte of the
// randomly-drawn trace ID — two trailing zero hex digits mark one trace in
// 256 sampled, keeping exemplar cardinality trivial while still surfacing
// live trace IDs on the latency histograms.
func New() Trace {
	traceID := randomHex(16)
	sampled := traceID[len(traceID)-1] == '0' && traceID[len(traceID)-2] == '0'

	return Trace{TraceID: traceID, SpanID: randomHex(8), Sampled: sampled}
}

// Child derives the next hop: same trace, fresh span.
func (t Trace) Child() Trace {
	return Trace{TraceID: t.TraceID, SpanID: randomHex(8), Sampled: t.Sampled}
}

// Header renders the trace back into a traceparent value.
func (t Trace) Header() string {
	flags := "00"
	if t.Sampled {
		flags = "01"
	}

	return fmt.Sprintf("00-%s-%s-%s", t.TraceID, t.SpanID, flags)
}

// WithContext attaches the trace to a request context.
func WithContext(ctx context.Context, trace Trace) context.Context {
	return context.WithValue(ctx, contextKey{}, trace)
}

// FromContext returns the trace attached to ctx, ok=false when none is.
func FromContext(ctx context.Context) (Trace, bool) {
	trace, ok := ctx.Value(contextKey{}).(Trace)

	return trace, ok
}

var randRead = rand.Read

func randomHex(bytes int) string {
	raw := make([]byte, bytes)
	if _, err := randRead(raw); err != nil {
		return strings.Repeat("0", bytes*2-1) + "1"
	}

	return hex.EncodeToString(raw)
}
