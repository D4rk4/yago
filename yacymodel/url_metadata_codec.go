package yacymodel

import (
	"bytes"
	"compress/flate"
	_ "embed"
	"fmt"
	"io"
)

const URLMetadataFormatV1 byte = 0x01

//go:embed url_metadata_dictionary.bin
var urlMetadataDictionary []byte

func EncodeURIMetadata(row URIMetadataRow) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(URLMetadataFormatV1)
	writer, err := flate.NewWriterDict(&buf, flate.BestCompression, urlMetadataDictionary)
	if err != nil {
		return nil, fmt.Errorf("%w: new flate writer: %w", ErrBadURLMetadata, err)
	}
	if _, err := writer.Write([]byte(row.String())); err != nil {
		return nil, fmt.Errorf("%w: flate write: %w", ErrBadURLMetadata, err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("%w: flate close: %w", ErrBadURLMetadata, err)
	}
	return buf.Bytes(), nil
}

func DecodeURIMetadata(data []byte) (URIMetadataRow, error) {
	if len(data) == 0 {
		return URIMetadataRow{}, fmt.Errorf("%w: empty url metadata value", ErrBadURLMetadata)
	}
	if data[0] != URLMetadataFormatV1 {
		return ParseURIMetadataRow(string(data))
	}
	reader := flate.NewReaderDict(bytes.NewReader(data[1:]), urlMetadataDictionary)
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return URIMetadataRow{}, fmt.Errorf("%w: flate read: %w", ErrBadURLMetadata, err)
	}
	return ParseURIMetadataRow(string(raw))
}
