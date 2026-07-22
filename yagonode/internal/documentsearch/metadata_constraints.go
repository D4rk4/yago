package documentsearch

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	maximumURLFilterBytes      = 2048
	maximumMetadataURLBytes    = 8192
	maximumMetadataAuthorBytes = 4096
)

type metadataConstraints struct {
	author    string
	fileType  string
	protocol  string
	urlFilter *regexp.Regexp
}

func metadataConstraintsFromRequest(
	req yagoproto.SearchRequest,
	operators queryOperators,
) (metadataConstraints, error) {
	constraints := metadataConstraints{
		author:   strings.ToLower(strings.TrimSpace(firstNonEmpty(operators.Author, req.Author))),
		fileType: normalizedFileType(firstNonEmpty(operators.FileType, req.FileType)),
		protocol: strings.ToLower(
			strings.TrimSpace(firstNonEmpty(operators.Protocol, req.Protocol)),
		),
	}
	filter := req.Filter
	if filter == "" || filter == ".*" {
		return constraints, nil
	}
	if len(filter) > maximumURLFilterBytes {
		return metadataConstraints{}, fmt.Errorf(
			"url filter exceeds %d bytes",
			maximumURLFilterBytes,
		)
	}
	compiled, err := regexp.Compile("\\A(" + filter + ")\\z")
	if err != nil {
		return metadataConstraints{}, fmt.Errorf("compile url filter: %w", err)
	}
	constraints.urlFilter = compiled

	return constraints, nil
}

func normalizedFileType(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "."))
}

func (c metadataConstraints) active() bool {
	return c.author != "" || c.fileType != "" || c.protocol != "" || c.urlFilter != nil
}

func (c metadataConstraints) matches(
	ctx context.Context,
	row yagomodel.URIMetadataRow,
) bool {
	if c.author != "" {
		author, ok := boundedMetadataProperty(
			ctx,
			row,
			yagomodel.URLMetaAuthor,
			maximumMetadataAuthorBytes,
		)
		if !ok || !strings.Contains(strings.ToLower(author), c.author) {
			return false
		}
	}
	if c.fileType == "" && c.protocol == "" && c.urlFilter == nil {
		return true
	}
	rawURL, ok := boundedMetadataProperty(
		ctx,
		row,
		yagomodel.URLMetaURL,
		maximumMetadataURLBytes,
	)
	if !ok || c.urlFilter != nil && !c.urlFilter.MatchString(rawURL) {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if c.protocol != "" && !strings.EqualFold(parsed.Scheme, c.protocol) {
		return false
	}
	if c.fileType != "" && normalizedFileType(path.Ext(parsed.Path)) != c.fileType {
		return false
	}

	return true
}

func boundedMetadataProperty(
	ctx context.Context,
	row yagomodel.URIMetadataRow,
	property string,
	maximumBytes int64,
) (string, bool) {
	encoded, found := row.Properties[property]
	if !found {
		return "", false
	}
	decoded, err := yagomodel.DecodeWireFormWithLimit(ctx, encoded, maximumBytes)

	return decoded, err == nil
}
