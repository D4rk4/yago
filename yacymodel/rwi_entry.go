package yacymodel

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	ColURLHash           = "h"
	ColLastModified      = "a"
	ColFreshUntil        = "s"
	ColTitleWordCount    = "u"
	ColTextWordCount     = "w"
	ColPhraseCount       = "p"
	ColDocType           = "d"
	ColLanguage          = "l"
	ColLocalLinkCount    = "x"
	ColExternalLinkCount = "y"
	ColURLLength         = "m"
	ColURLComponentCount = "n"
	ColWordType          = "g"
	ColFlags             = "z"
	ColHitCount          = "c"
	ColTextPosition      = "t"
	ColPhraseRelativePos = "r"
	ColPhrasePosition    = "o"
	ColWordDistance      = "i"
	ColReserve           = "k"
	corruptMarker        = "[B@"
	propertyOpen         = '{'
	propertyClose        = '}'
	requiredColumn       = ColLocalLinkCount
)

var ErrBadRWIEntry = errors.New("bad rwi entry")

type RWIEntry struct {
	WordHash   Hash
	Properties map[string]string
}

func (e RWIEntry) URLHash() (Hash, error) {
	return ParseHash(e.Properties[ColURLHash])
}

func ParseRWIEntry(line string) (RWIEntry, error) {
	open := strings.IndexByte(line, propertyOpen)
	if open < 0 || !strings.HasSuffix(line, string(propertyClose)) {
		return RWIEntry{}, fmt.Errorf("%w: missing property form", ErrBadRWIEntry)
	}
	wordHash, err := ParseHash(line[:open])
	if err != nil {
		return RWIEntry{}, fmt.Errorf("%w: word hash: %w", ErrBadRWIEntry, err)
	}
	props, err := parsePropertyPairs(line[open+1 : len(line)-1])
	if err != nil {
		return RWIEntry{}, fmt.Errorf("%w: %w", ErrBadRWIEntry, err)
	}
	props, err = normalizeRWIProperties(props)
	if err != nil {
		return RWIEntry{}, fmt.Errorf("%w: %w", ErrBadRWIEntry, err)
	}
	if err := validateRWIProperties(props); err != nil {
		return RWIEntry{}, fmt.Errorf("%w: %w", ErrBadRWIEntry, err)
	}
	return RWIEntry{WordHash: wordHash, Properties: props}, nil
}

func (e RWIEntry) String() string {
	keys := make([]string, 0, len(e.Properties))
	for k := range e.Properties {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString(string(e.WordHash))
	b.WriteByte(propertyOpen)
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(e.Properties[k])
	}
	b.WriteByte(propertyClose)
	return b.String()
}

func AcceptableRWILine(line string) bool {
	if !strings.ContainsRune(line, propertyOpen) ||
		!strings.Contains(line, requiredColumn+"=") ||
		strings.Contains(line, corruptMarker) {
		return false
	}
	entry, err := ParseRWIEntry(line)
	if err != nil {
		return false
	}
	_, err = entry.URLHash()
	return err == nil
}
