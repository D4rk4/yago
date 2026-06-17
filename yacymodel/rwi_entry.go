package yacymodel

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// WordReference column keys carried in an RWI entry's property form.
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

// ErrBadRWIEntry reports an RWI posting line that is not well-formed.
var ErrBadRWIEntry = errors.New("bad rwi entry")

// RWIEntry is one reverse-word-index posting: a word hash and the property-form
// WordReferenceRow that records a URL's relation to that word.
type RWIEntry struct {
	WordHash   Hash
	Properties map[string]string
}

// URLHash returns the posting's URL hash field validated as a Hash.
func (e RWIEntry) URLHash() (Hash, error) {
	return ParseHash(e.Properties[ColURLHash])
}

// ParseRWIEntry parses one posting line of the form wordHash{col=value,...}.
func ParseRWIEntry(line string) (RWIEntry, error) {
	open := strings.IndexByte(line, propertyOpen)
	if open < 0 || !strings.HasSuffix(line, string(propertyClose)) {
		return RWIEntry{}, fmt.Errorf("%w: missing property form", ErrBadRWIEntry)
	}
	wordHash, err := ParseHash(line[:open])
	if err != nil {
		return RWIEntry{}, fmt.Errorf("%w: word hash: %w", ErrBadRWIEntry, err)
	}
	props := make(map[string]string)
	body := line[open+1 : len(line)-1]
	for pair := range strings.SplitSeq(body, ",") {
		if pair == "" {
			continue
		}
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return RWIEntry{}, fmt.Errorf("%w: property %q", ErrBadRWIEntry, pair)
		}
		props[key] = value
	}
	return RWIEntry{WordHash: wordHash, Properties: props}, nil
}

// String renders the posting line with property keys in sorted order.
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

// AcceptableRWILine reports whether a receiver should accept a raw posting line:
// it must carry a property form and the required column, must not carry the
// corruption marker, and its word and URL hashes must both be well-formed.
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
