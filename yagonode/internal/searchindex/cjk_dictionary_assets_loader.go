package searchindex

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"sync"

	"github.com/blevesearch/vellum"
)

type cjkChineseDictionary struct {
	lexicon          *vellum.FST
	conversion       *vellum.FST
	conversionOutput []byte
}

var loadCJKChineseDictionary = sync.OnceValues(func() (*cjkChineseDictionary, error) {
	return loadCJKChineseDictionaryAssets(
		cjkChineseLexiconEncoded,
		cjkChineseConversionEncoded,
		cjkChineseConversionOutputsEncoded,
	)
})

var loadCJKJapaneseDictionary = sync.OnceValues(func() (*vellum.FST, error) {
	return loadCJKJapaneseDictionaryAsset(cjkJapaneseLexiconEncoded)
})

func loadCJKChineseDictionaryAssets(
	lexiconAsset string,
	conversionAsset string,
	outputAsset string,
) (*cjkChineseDictionary, error) {
	lexicon, err := loadCJKFST(lexiconAsset)
	if err != nil {
		return nil, fmt.Errorf("load Chinese lexicon: %w", err)
	}
	conversion, err := loadCJKFST(conversionAsset)
	if err != nil {
		return nil, fmt.Errorf("load Chinese conversion: %w", err)
	}
	outputs, err := decodeCJKDictionaryAsset(outputAsset)
	if err != nil {
		return nil, fmt.Errorf("load Chinese conversion outputs: %w", err)
	}

	return &cjkChineseDictionary{
		lexicon:          lexicon,
		conversion:       conversion,
		conversionOutput: outputs,
	}, nil
}

func loadCJKJapaneseDictionaryAsset(encoded string) (*vellum.FST, error) {
	lexicon, err := loadCJKFST(encoded)
	if err != nil {
		return nil, fmt.Errorf("load Japanese lexicon: %w", err)
	}

	return lexicon, nil
}

func loadCJKFST(encoded string) (*vellum.FST, error) {
	data, err := decodeCJKDictionaryAsset(encoded)
	if err != nil {
		return nil, err
	}
	lexicon, err := vellum.Load(data)
	if err != nil {
		return nil, fmt.Errorf("decode FST: %w", err)
	}

	return lexicon, nil
}

func decodeCJKDictionaryAsset(encoded string) ([]byte, error) {
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return nil, fmt.Errorf("read gzip: %w", err)
	}

	return data, nil
}
