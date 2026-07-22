package yagomodel

import (
	"errors"
	"fmt"
	"strings"
)

var ErrBadWordReference = errors.New("bad word reference")

type WordReference struct {
	Properties map[string]string
}

type wordReferenceColumn struct {
	name         string
	defaultValue string
}

var wordReferenceColumns = [...]wordReferenceColumn{
	{ColURLHash, ""},
	{ColLastModified, "0"},
	{ColFreshUntil, "0"},
	{ColTitleWordCount, "0"},
	{ColTextWordCount, "0"},
	{ColPhraseCount, "0"},
	{ColDocType, "0"},
	{ColLanguage, "en"},
	{ColLocalLinkCount, "0"},
	{ColExternalLinkCount, "0"},
	{ColURLLength, "0"},
	{ColURLComponentCount, "0"},
	{ColWordType, "0"},
	{ColFlags, "AAAAAA"},
	{ColHitCount, "0"},
	{ColTextPosition, "0"},
	{ColPhraseRelativePos, "0"},
	{ColPhrasePosition, "0"},
	{ColWordDistance, "0"},
	{ColReserve, "0"},
}

func ParseWordReference(propertyForm string) (WordReference, error) {
	if len(propertyForm) < 2 || propertyForm[0] != propertyOpen ||
		propertyForm[len(propertyForm)-1] != propertyClose {
		return WordReference{}, fmt.Errorf("%w: missing property form", ErrBadWordReference)
	}
	tokens := strings.Split(propertyForm[1:len(propertyForm)-1], ",")
	if len(tokens) != len(wordReferenceColumns) {
		return WordReference{}, fmt.Errorf(
			"%w: %d columns, want %d",
			ErrBadWordReference,
			len(tokens),
			len(wordReferenceColumns),
		)
	}
	properties := make(map[string]string, len(wordReferenceColumns))
	for position, column := range wordReferenceColumns {
		name, value, ok := strings.Cut(strings.TrimSpace(tokens[position]), "=")
		if !ok || strings.TrimSpace(name) != column.name {
			return WordReference{}, fmt.Errorf(
				"%w: column %d is not %s",
				ErrBadWordReference,
				position,
				column.name,
			)
		}
		properties[column.name] = strings.TrimSpace(value)
	}
	normalized, err := normalizeRWIProperties(properties)
	if err != nil {
		return WordReference{}, fmt.Errorf("%w: %w", ErrBadWordReference, err)
	}
	if err := validateRWIProperties(normalized); err != nil {
		return WordReference{}, fmt.Errorf("%w: %w", ErrBadWordReference, err)
	}

	return WordReference{Properties: normalized}, nil
}

func (reference WordReference) URLHash() URLHash {
	hash, _ := ParseURLHash(reference.Properties[ColURLHash])

	return hash
}

func WordReferencePropertyForm(posting RWIPosting) string {
	var propertyForm strings.Builder
	propertyForm.WriteByte(propertyOpen)
	for position, column := range wordReferenceColumns {
		if position > 0 {
			propertyForm.WriteByte(',')
		}
		propertyForm.WriteString(column.name)
		propertyForm.WriteByte('=')
		value := posting.Properties[column.name]
		if value == "" {
			value = column.defaultValue
		}
		propertyForm.WriteString(value)
	}
	propertyForm.WriteByte(propertyClose)

	return propertyForm.String()
}
