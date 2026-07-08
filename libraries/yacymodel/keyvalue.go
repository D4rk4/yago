package yacymodel

import (
	"errors"
	"slices"
	"strings"
)

var ErrBadMessage = errors.New("bad message")

type Message map[string]string

func ParseMessage(data string) (Message, error) {
	msg := make(Message)
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		pos := unescapedEquals(line)
		if pos <= 0 {
			continue
		}
		key := unescapeMessagePart(strings.TrimSpace(line[:pos]))
		value := unescapeMessagePart(strings.TrimSpace(line[pos+1:]))
		msg[key] = value
	}
	return msg, nil
}

func unescapedEquals(line string) int {
	pos := 0
	for {
		next := strings.IndexByte(line[pos+1:], '=')
		if next < 0 {
			return -1
		}
		pos += next + 1
		if line[pos-1] != '\\' {
			return pos
		}
	}
}

func unescapeMessagePart(s string) string {
	replacer := strings.NewReplacer(
		`\\`, `\`,
		`\n`, "\n",
		`\=`, `=`,
	)
	return replacer.Replace(s)
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
