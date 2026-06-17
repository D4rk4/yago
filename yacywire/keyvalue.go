package yacywire

import (
	"slices"
	"strings"
)

// Message is a YaCy key=value wire payload: the field map of a DHT request or
// the line set of a response.
type Message map[string]string

// ParseMessage decodes key=value lines separated by CRLF or LF. Blank lines and
// lines without a key are ignored. A later line wins when a key repeats.
func ParseMessage(data string) Message {
	msg := make(Message)
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			continue
		}
		msg[key] = value
	}
	return msg
}

// Encode renders the message as LF-terminated key=value lines with keys in
// sorted order, so identical messages encode identically.
func (m Message) Encode() string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
		b.WriteByte('\n')
	}
	return b.String()
}
