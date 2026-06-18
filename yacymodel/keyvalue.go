package yacymodel

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

var ErrBadMessage = errors.New("bad message")

type Message map[string]string

func ParseMessage(data string) (Message, error) {
	msg := make(Message)
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("%w: %q", ErrBadMessage, line)
		}
		msg[key] = value
	}
	return msg, nil
}

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
