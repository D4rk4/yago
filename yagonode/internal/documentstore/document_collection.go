package documentstore

import (
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const bucketName vault.Name = "documents"

type documentCodec struct{}

func (documentCodec) Encode(doc Document) ([]byte, error) {
	data, _ := json.Marshal(doc)
	return data, nil
}

func (documentCodec) Decode(raw []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Document{}, fmt.Errorf("unmarshal document: %w", err)
	}

	return doc, nil
}

func registerCollection(v *vault.Vault) (*vault.Collection[Document], error) {
	collection, err := vault.Register(v, bucketName, documentCodec{})
	if err != nil {
		return nil, fmt.Errorf("register document collection: %w", err)
	}

	return collection, nil
}
