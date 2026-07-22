package searchremote

import (
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
)

const maximumRemoteWordReferenceBytes = 1024

var errRemoteResourceWordReference = errors.New("invalid remote resource word reference")

func validateRemoteResourceWordReference(row yagomodel.URIMetadataRow) error {
	_, err := validatedRemoteResourceURLHash(row)

	return err
}

func validatedRemoteResourceURLHash(
	row yagomodel.URIMetadataRow,
) (yagomodel.URLHash, error) {
	encoded := row.Properties[yagomodel.URLMetaWordReference]
	if encoded == "" {
		return "", fmt.Errorf("%w: missing wi", errRemoteResourceWordReference)
	}
	if len(encoded) > maximumRemoteWordReferenceBytes*2 {
		return "", fmt.Errorf("%w: wi exceeds limit", errRemoteResourceWordReference)
	}
	decoded, err := yagomodel.Decode(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: decode wi: %w", errRemoteResourceWordReference, err)
	}
	if len(decoded) > maximumRemoteWordReferenceBytes {
		return "", fmt.Errorf("%w: wi exceeds limit", errRemoteResourceWordReference)
	}
	reference, err := yagomodel.ParseWordReference(string(decoded))
	if err != nil {
		return "", fmt.Errorf("%w: %w", errRemoteResourceWordReference, err)
	}
	resourceHash, err := row.URLHash()
	if err != nil {
		return "", fmt.Errorf("%w: resource hash: %w", errRemoteResourceWordReference, err)
	}
	referenceHash := reference.URLHash()
	if referenceHash != resourceHash {
		return "", fmt.Errorf(
			"%w: wi hash %s does not match resource hash %s",
			errRemoteResourceWordReference,
			referenceHash,
			resourceHash,
		)
	}

	return resourceHash, nil
}

func validateRemoteResourceWordReferences(rows []yagomodel.URIMetadataRow) error {
	for position, row := range rows {
		if err := validateRemoteResourceWordReference(row); err != nil {
			return fmt.Errorf("resource %d: %w", position, err)
		}
	}

	return nil
}
