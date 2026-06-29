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
	propertyOpen         = '{'
	propertyClose        = '}'
)

var ErrBadRWIPosting = errors.New("bad rwi posting")

type RWIPosting struct {
	WordHash   Hash
	Properties map[string]string
}

type RWIPostingID struct {
	WordHash Hash
	URLHash  Hash
}

type WordPostings struct {
	WordHash Hash
	Postings []RWIPosting
}

func (e RWIPosting) URLHash() (URLHash, error) {
	return ParseURLHash(e.Properties[ColURLHash])
}

func ParseRWIPosting(line string) (RWIPosting, error) {
	open := strings.IndexByte(line, propertyOpen)
	if open < 0 || !strings.HasSuffix(line, string(propertyClose)) {
		return RWIPosting{}, fmt.Errorf("%w: missing property form", ErrBadRWIPosting)
	}
	wordHash, err := ParseHash(line[:open])
	if err != nil {
		return RWIPosting{}, fmt.Errorf("%w: word hash: %w", ErrBadRWIPosting, err)
	}
	props, err := parsePropertyPairs(line[open+1 : len(line)-1])
	if err != nil {
		return RWIPosting{}, fmt.Errorf("%w: %w", ErrBadRWIPosting, err)
	}
	props, err = normalizeRWIProperties(props)
	if err != nil {
		return RWIPosting{}, fmt.Errorf("%w: %w", ErrBadRWIPosting, err)
	}
	if err := validateRWIProperties(props); err != nil {
		return RWIPosting{}, fmt.Errorf("%w: %w", ErrBadRWIPosting, err)
	}
	return RWIPosting{WordHash: wordHash, Properties: props}, nil
}

func (e RWIPosting) String() string {
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
