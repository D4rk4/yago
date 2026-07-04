package yagomodel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	URLMetaHash           = "hash"
	URLMetaHashAlt        = "h"
	URLMetaURL            = "url"
	URLMetaColDescription = "descr"
	URLMetaAuthor         = "author"
	URLMetaReferrer       = "referrer"
)

var ErrBadURLMetadata = errors.New("bad url metadata")

type URIMetadataRow struct {
	Properties map[string]string
}

func ParseURIMetadataRow(row string) (URIMetadataRow, error) {
	if len(row) < 2 || row[0] != urlMetadataOpen || row[len(row)-1] != urlMetadataClose {
		return URIMetadataRow{}, fmt.Errorf("%w: missing property form", ErrBadURLMetadata)
	}
	props, err := parsePropertyPairs(row[1 : len(row)-1])
	if err != nil {
		return URIMetadataRow{}, fmt.Errorf("%w: %w", ErrBadURLMetadata, err)
	}
	if len(props) == 0 {
		return URIMetadataRow{}, fmt.Errorf("%w: empty row", ErrBadURLMetadata)
	}
	if err := validateURLMetadataProperties(props); err != nil {
		return URIMetadataRow{}, fmt.Errorf("%w: %w", ErrBadURLMetadata, err)
	}
	return URIMetadataRow{Properties: props}, nil
}

func (r URIMetadataRow) URLHash() (URLHash, error) {
	return urlMetadataHash(r.Properties)
}

func (r URIMetadataRow) Title(ctx context.Context) (string, error) {
	return DecodeWireForm(ctx, r.Properties[URLMetaColDescription])
}

func (r URIMetadataRow) String() string {
	keys := make([]string, 0, len(r.Properties))
	for k := range r.Properties {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteByte(urlMetadataOpen)
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(r.Properties[k])
	}
	b.WriteByte(urlMetadataClose)
	return b.String()
}
