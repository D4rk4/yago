package urlmeta

import (
	"bytes"
	"compress/flate"
	_ "embed"
	"fmt"
	"io"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const storedURLMetadataFormatV1 byte = 0x01

//go:embed url_metadata_dictionary.bin
var urlMetadataDictionary []byte

func encodeStoredURLMetadata(row yacymodel.URIMetadataRow) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(storedURLMetadataFormatV1)
	writer, err := flate.NewWriterDict(&buf, flate.BestCompression, urlMetadataDictionary)
	if err != nil {
		return nil, fmt.Errorf("%w: new flate writer: %w", yacymodel.ErrBadURLMetadata, err)
	}
	if _, err := writer.Write([]byte(row.String())); err != nil {
		return nil, fmt.Errorf("%w: flate write: %w", yacymodel.ErrBadURLMetadata, err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("%w: flate close: %w", yacymodel.ErrBadURLMetadata, err)
	}
	return buf.Bytes(), nil
}

func decodeStoredURLMetadata(data []byte) (yacymodel.URIMetadataRow, error) {
	if len(data) == 0 {
		return yacymodel.URIMetadataRow{}, fmt.Errorf(
			"%w: empty url metadata value",
			yacymodel.ErrBadURLMetadata,
		)
	}
	if data[0] != storedURLMetadataFormatV1 {
		row, err := yacymodel.ParseURIMetadataRow(string(data))
		if err != nil {
			return yacymodel.URIMetadataRow{}, fmt.Errorf("parse property form: %w", err)
		}
		return row, nil
	}
	reader := flate.NewReaderDict(bytes.NewReader(data[1:]), urlMetadataDictionary)
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return yacymodel.URIMetadataRow{}, fmt.Errorf(
			"%w: flate read: %w",
			yacymodel.ErrBadURLMetadata,
			err,
		)
	}
	row, err := yacymodel.ParseURIMetadataRow(string(raw))
	if err != nil {
		return yacymodel.URIMetadataRow{}, fmt.Errorf("parse property form: %w", err)
	}
	return row, nil
}
