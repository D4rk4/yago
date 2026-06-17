package yacywire

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// URIMetadataRow column keys carrying the URL hash. YaCy commonly uses hash,
// and some versions and paths use h.
const (
	URLMetaHash    = "hash"
	URLMetaHashAlt = "h"
)

// ErrBadURLMetadata reports a URL metadata row that is not well-formed.
var ErrBadURLMetadata = errors.New("bad url metadata")

// URIMetadataRow is an indexed URL's metadata in comma-separated property form.
type URIMetadataRow struct {
	Properties map[string]string
}

// ParseURIMetadataRow parses one comma-separated col=value metadata row.
func ParseURIMetadataRow(row string) (URIMetadataRow, error) {
	props := make(map[string]string)
	for pair := range strings.SplitSeq(row, ",") {
		if pair == "" {
			continue
		}
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return URIMetadataRow{}, fmt.Errorf("%w: property %q", ErrBadURLMetadata, pair)
		}
		props[key] = value
	}
	if len(props) == 0 {
		return URIMetadataRow{}, fmt.Errorf("%w: empty row", ErrBadURLMetadata)
	}
	return URIMetadataRow{Properties: props}, nil
}

// URLHash returns the row's URL hash, preferring the hash column and falling
// back to h, validated as a Hash.
func (r URIMetadataRow) URLHash() (Hash, error) {
	if v, ok := r.Properties[URLMetaHash]; ok {
		return ParseHash(v)
	}
	if v, ok := r.Properties[URLMetaHashAlt]; ok {
		return ParseHash(v)
	}
	return "", fmt.Errorf("%w: no url hash", ErrBadURLMetadata)
}

// String renders the row with property keys in sorted order.
func (r URIMetadataRow) String() string {
	keys := make([]string, 0, len(r.Properties))
	for k := range r.Properties {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(r.Properties[k])
	}
	return b.String()
}
