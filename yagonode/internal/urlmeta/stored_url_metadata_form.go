package urlmeta

import (
	"bytes"
	"compress/flate"
	_ "embed"
	"fmt"
	"io"

	"github.com/D4rk4/yago/yagomodel"
)

const storedURLMetadataFormatV1 byte = 0x01

//go:embed url_metadata_dictionary.bin
var urlMetadataDictionary []byte

func encodeStoredURLMetadata(row yagomodel.URIMetadataRow) []byte {
	var buf bytes.Buffer
	buf.WriteByte(storedURLMetadataFormatV1)
	writer, _ := flate.NewWriterDict(&buf, flate.BestCompression, urlMetadataDictionary)
	_, _ = writer.Write([]byte(row.String()))
	_ = writer.Close()
	return buf.Bytes()
}

func decodeStoredURLMetadata(data []byte) (yagomodel.URIMetadataRow, error) {
	if len(data) == 0 {
		return yagomodel.URIMetadataRow{}, fmt.Errorf(
			"%w: empty url metadata value",
			yagomodel.ErrBadURLMetadata,
		)
	}
	if data[0] != storedURLMetadataFormatV1 {
		row, err := yagomodel.ParseURIMetadataRow(string(data))
		if err != nil {
			return yagomodel.URIMetadataRow{}, fmt.Errorf("parse property form: %w", err)
		}
		return row, nil
	}
	reader := flate.NewReaderDict(bytes.NewReader(data[1:]), urlMetadataDictionary)
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return yagomodel.URIMetadataRow{}, fmt.Errorf(
			"%w: flate read: %w",
			yagomodel.ErrBadURLMetadata,
			err,
		)
	}
	row, err := yagomodel.ParseURIMetadataRow(string(raw))
	if err != nil {
		return yagomodel.URIMetadataRow{}, fmt.Errorf("parse property form: %w", err)
	}
	return row, nil
}
