package rwi

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"slices"
	"strconv"

	"github.com/D4rk4/yago/yagomodel"
)

const storedPostingFormatV1 byte = 0x01

var storedPostingColumns = []string{
	yagomodel.ColURLHash,
	yagomodel.ColLastModified,
	yagomodel.ColFreshUntil,
	yagomodel.ColTitleWordCount,
	yagomodel.ColTextWordCount,
	yagomodel.ColPhraseCount,
	yagomodel.ColDocType,
	yagomodel.ColLanguage,
	yagomodel.ColLocalLinkCount,
	yagomodel.ColExternalLinkCount,
	yagomodel.ColURLLength,
	yagomodel.ColURLComponentCount,
	yagomodel.ColWordType,
	yagomodel.ColFlags,
	yagomodel.ColHitCount,
	yagomodel.ColTextPosition,
	yagomodel.ColPhraseRelativePos,
	yagomodel.ColPhrasePosition,
	yagomodel.ColWordDistance,
	yagomodel.ColReserve,
}

var storedPostingColumnIndex = indexStoredPostingColumns()

func indexStoredPostingColumns() map[string]int {
	index := make(map[string]int, len(storedPostingColumns))
	for position, column := range storedPostingColumns {
		index[column] = position
	}
	return index
}

func encodeStoredPosting(entry yagomodel.RWIPosting) []byte {
	var buf bytes.Buffer
	buf.WriteByte(storedPostingFormatV1)

	var mask uint32
	for position, column := range storedPostingColumns {
		if _, ok := entry.Properties[column]; ok {
			mask |= 1 << uint(position)
		}
	}
	var maskBytes [4]byte
	binary.LittleEndian.PutUint32(maskBytes[:], mask)
	buf.Write(maskBytes[:])

	for _, column := range storedPostingColumns {
		value, ok := entry.Properties[column]
		if !ok {
			continue
		}
		encodeStoredPostingColumn(&buf, column, value)
	}

	encodeStoredPostingExtras(&buf, entry.Properties)
	return buf.Bytes()
}

func encodeStoredPostingColumn(buf *bytes.Buffer, column, value string) {
	switch column {
	case yagomodel.ColURLHash:
		buf.WriteString(value)
	case yagomodel.ColLanguage:
		writeLengthPrefixed(buf, []byte(value))
	case yagomodel.ColFlags:
		raw, _ := yagomodel.Decode(value)
		writeLengthPrefixed(buf, raw)
	default:
		number, _ := strconv.ParseUint(value, 10, 64)
		writeUvarint(buf, number)
	}
}

func encodeStoredPostingExtras(buf *bytes.Buffer, props map[string]string) {
	var extras []string
	for key := range props {
		if _, ok := storedPostingColumnIndex[key]; !ok {
			extras = append(extras, key)
		}
	}
	slices.Sort(extras)
	writeUvarint(buf, uint64(len(extras)))
	for _, key := range extras {
		writeLengthPrefixed(buf, []byte(key))
		writeLengthPrefixed(buf, []byte(props[key]))
	}
}

func decodeStoredPosting(wordHash yagomodel.Hash, data []byte) (yagomodel.RWIPosting, error) {
	if len(data) == 0 {
		return yagomodel.RWIPosting{}, fmt.Errorf(
			"%w: empty posting value",
			yagomodel.ErrBadRWIPosting,
		)
	}
	if data[0] != storedPostingFormatV1 {
		entry, err := yagomodel.ParseRWIPosting(string(data))
		if err != nil {
			return yagomodel.RWIPosting{}, fmt.Errorf("parse property form: %w", err)
		}
		return entry, nil
	}
	return decodeStoredPostingV1(wordHash, data[1:])
}

func decodeStoredPostingV1(wordHash yagomodel.Hash, data []byte) (yagomodel.RWIPosting, error) {
	reader := bytes.NewReader(data)
	var mask uint32
	if err := binary.Read(reader, binary.LittleEndian, &mask); err != nil {
		return yagomodel.RWIPosting{}, fmt.Errorf(
			"%w: posting mask: %w",
			yagomodel.ErrBadRWIPosting,
			err,
		)
	}
	props := make(map[string]string)
	for position, column := range storedPostingColumns {
		if mask&(1<<uint(position)) == 0 {
			continue
		}
		value, err := decodeStoredPostingColumn(reader, column)
		if err != nil {
			return yagomodel.RWIPosting{}, fmt.Errorf(
				"%w: column %s: %w",
				yagomodel.ErrBadRWIPosting,
				column,
				err,
			)
		}
		props[column] = value
	}
	if err := decodeStoredPostingExtras(reader, props); err != nil {
		return yagomodel.RWIPosting{}, fmt.Errorf("%w: %w", yagomodel.ErrBadRWIPosting, err)
	}
	return yagomodel.RWIPosting{WordHash: wordHash, Properties: props}, nil
}

func decodeStoredPostingColumn(reader *bytes.Reader, column string) (string, error) {
	switch column {
	case yagomodel.ColURLHash:
		raw := make([]byte, yagomodel.HashLength)
		if _, err := io.ReadFull(reader, raw); err != nil {
			return "", fmt.Errorf("%w: read hash: %w", yagomodel.ErrBadRWIPosting, err)
		}
		return string(raw), nil
	case yagomodel.ColLanguage:
		raw, err := readLengthPrefixed(reader)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	case yagomodel.ColFlags:
		raw, err := readLengthPrefixed(reader)
		if err != nil {
			return "", err
		}
		return yagomodel.Encode(raw), nil
	default:
		number, err := binary.ReadUvarint(reader)
		if err != nil {
			return "", fmt.Errorf("%w: read cardinal: %w", yagomodel.ErrBadRWIPosting, err)
		}
		return yagomodel.FormatRWICardinal(number), nil
	}
}

func decodeStoredPostingExtras(reader *bytes.Reader, props map[string]string) error {
	count, err := binary.ReadUvarint(reader)
	if err != nil {
		return fmt.Errorf("%w: read extras count: %w", yagomodel.ErrBadRWIPosting, err)
	}
	if remaining := reader.Len(); remaining < 0 || count > uint64(remaining) {
		return fmt.Errorf(
			"%w: extras count %d exceeds remaining bytes",
			yagomodel.ErrBadRWIPosting,
			count,
		)
	}
	for range count {
		key, err := readLengthPrefixed(reader)
		if err != nil {
			return err
		}
		value, err := readLengthPrefixed(reader)
		if err != nil {
			return err
		}
		props[string(key)] = string(value)
	}
	return nil
}

func writeUvarint(buf *bytes.Buffer, number uint64) {
	var tmp [binary.MaxVarintLen64]byte
	written := binary.PutUvarint(tmp[:], number)
	buf.Write(tmp[:written])
}

func writeLengthPrefixed(buf *bytes.Buffer, data []byte) {
	writeUvarint(buf, uint64(len(data)))
	buf.Write(data)
}

func readLengthPrefixed(reader *bytes.Reader) ([]byte, error) {
	length, err := binary.ReadUvarint(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: read length: %w", yagomodel.ErrBadRWIPosting, err)
	}
	if remaining := reader.Len(); remaining < 0 || length > uint64(remaining) {
		return nil, fmt.Errorf(
			"%w: length %d exceeds remaining bytes",
			yagomodel.ErrBadRWIPosting,
			length,
		)
	}
	raw := make([]byte, length)
	_, _ = io.ReadFull(reader, raw)
	return raw, nil
}
