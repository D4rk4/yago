package documentstore

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	bucketName                   vault.Name = "documents"
	orderedDocumentBucketName    vault.Name = "documents_ordered"
	documentLocationBucketName   vault.Name = "document_locations"
	documentAdmissionBucketName  vault.Name = "document_admissions"
	orderedDocumentAdmissionSize            = 8
)

var documentAdmissionHighWaterKey = vault.Key("high_water")

type documentCodec struct{}

func (documentCodec) Encode(doc Document) ([]byte, error) {
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal document: %w", err)
	}

	return data, nil
}

func (documentCodec) Decode(raw []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Document{}, fmt.Errorf("unmarshal document: %w", err)
	}

	return doc, nil
}

type documentLocationCodec struct{}

func (documentLocationCodec) Encode(admission uint64) ([]byte, error) {
	return encodeOrderedDocumentAdmission(admission)
}

func (documentLocationCodec) Decode(raw []byte) (uint64, error) {
	return decodeOrderedDocumentAdmission(raw)
}

func encodeOrderedDocumentAdmission(admission uint64) ([]byte, error) {
	if admission == 0 {
		return nil, fmt.Errorf("ordered document admission must be positive")
	}
	raw := make([]byte, orderedDocumentAdmissionSize)
	binary.BigEndian.PutUint64(raw, admission)

	return raw, nil
}

func decodeOrderedDocumentAdmission(raw []byte) (uint64, error) {
	if len(raw) != orderedDocumentAdmissionSize {
		return 0, fmt.Errorf("ordered document admission has %d bytes", len(raw))
	}
	admission := binary.BigEndian.Uint64(raw)
	if admission == 0 {
		return 0, fmt.Errorf("ordered document admission must be positive")
	}

	return admission, nil
}

func orderedDocumentKey(admission uint64, normalizedURL string) (vault.Key, error) {
	if normalizedURL == "" {
		return nil, fmt.Errorf("ordered document URL must not be empty")
	}
	if len(normalizedURL) > yagomodel.MaximumURLIdentityBytes {
		return nil, fmt.Errorf("ordered document URL has %d bytes", len(normalizedURL))
	}
	encodedAdmission, err := encodeOrderedDocumentAdmission(admission)
	if err != nil {
		return nil, err
	}
	key := make(vault.Key, 0, len(encodedAdmission)+len(normalizedURL))
	key = append(key, encodedAdmission...)
	key = append(key, normalizedURL...)

	return key, nil
}

func decodeOrderedDocumentKey(key vault.Key) (uint64, string, error) {
	if len(key) <= orderedDocumentAdmissionSize {
		return 0, "", fmt.Errorf("ordered document key has %d bytes", len(key))
	}
	normalizedURL := string(key[orderedDocumentAdmissionSize:])
	if len(normalizedURL) > yagomodel.MaximumURLIdentityBytes {
		return 0, "", fmt.Errorf("ordered document URL has %d bytes", len(normalizedURL))
	}
	admission, err := decodeOrderedDocumentAdmission(key[:orderedDocumentAdmissionSize])
	if err != nil {
		return 0, "", err
	}

	return admission, normalizedURL, nil
}

func registerDocumentCollections(
	v *vault.Vault,
) (
	*vault.Keyspace[Document],
	*vault.Keyspace[Document],
	*vault.Keyspace[uint64],
	*vault.Keyspace[uint64],
	error,
) {
	legacyDocuments, err := vault.RegisterKeyspace(v, bucketName, documentCodec{})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("register legacy documents: %w", err)
	}
	orderedDocuments, err := vault.RegisterKeyspace(
		v,
		orderedDocumentBucketName,
		documentCodec{},
	)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("register ordered documents: %w", err)
	}
	documentLocations, err := vault.RegisterKeyspace(
		v,
		documentLocationBucketName,
		documentLocationCodec{},
	)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("register document locations: %w", err)
	}
	documentAdmissions, err := vault.RegisterKeyspace(
		v,
		documentAdmissionBucketName,
		documentLocationCodec{},
	)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("register document admissions: %w", err)
	}

	return legacyDocuments, orderedDocuments, documentLocations, documentAdmissions, nil
}
