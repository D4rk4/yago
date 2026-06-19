package yacymodel

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"slices"
	"strconv"
)

const RWIPostingFormatV1 byte = 0x01

var rwiPostingColumns = []string{
	ColURLHash, ColLastModified, ColFreshUntil, ColTitleWordCount, ColTextWordCount,
	ColPhraseCount, ColDocType, ColLanguage, ColLocalLinkCount, ColExternalLinkCount,
	ColURLLength, ColURLComponentCount, ColWordType, ColFlags, ColHitCount,
	ColTextPosition, ColPhraseRelativePos, ColPhrasePosition, ColWordDistance, ColReserve,
}

var rwiPostingColumnIndex = indexRWIPostingColumns()

func indexRWIPostingColumns() map[string]int {
	index := make(map[string]int, len(rwiPostingColumns))
	for position, column := range rwiPostingColumns {
		index[column] = position
	}
	return index
}

func EncodeRWIPosting(entry RWIEntry) []byte {
	var buf bytes.Buffer
	buf.WriteByte(RWIPostingFormatV1)

	var mask uint32
	for position, column := range rwiPostingColumns {
		if _, ok := entry.Properties[column]; ok {
			mask |= 1 << uint(position)
		}
	}
	var maskBytes [4]byte
	binary.LittleEndian.PutUint32(maskBytes[:], mask)
	buf.Write(maskBytes[:])

	for _, column := range rwiPostingColumns {
		value, ok := entry.Properties[column]
		if !ok {
			continue
		}
		encodeRWIColumn(&buf, column, value)
	}

	encodeRWIExtras(&buf, entry.Properties)
	return buf.Bytes()
}

func encodeRWIColumn(buf *bytes.Buffer, column, value string) {
	switch column {
	case ColURLHash:
		buf.WriteString(value)
	case ColLanguage:
		writeLengthPrefixed(buf, []byte(value))
	case ColFlags:
		raw, _ := Decode(value)
		writeLengthPrefixed(buf, raw)
	default:
		number, _ := strconv.ParseUint(value, 10, 64)
		writeUvarint(buf, number)
	}
}

func encodeRWIExtras(buf *bytes.Buffer, props map[string]string) {
	var extras []string
	for key := range props {
		if _, ok := rwiPostingColumnIndex[key]; !ok {
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

func DecodeRWIPosting(wordHash Hash, data []byte) (RWIEntry, error) {
	if len(data) == 0 {
		return RWIEntry{}, fmt.Errorf("%w: empty posting value", ErrBadRWIEntry)
	}
	if data[0] != RWIPostingFormatV1 {
		return ParseRWIEntry(string(data))
	}
	return decodeRWIPostingV1(wordHash, data[1:])
}

func decodeRWIPostingV1(wordHash Hash, data []byte) (RWIEntry, error) {
	reader := bytes.NewReader(data)
	var mask uint32
	if err := binary.Read(reader, binary.LittleEndian, &mask); err != nil {
		return RWIEntry{}, fmt.Errorf("%w: posting mask: %w", ErrBadRWIEntry, err)
	}
	props := make(map[string]string)
	for position, column := range rwiPostingColumns {
		if mask&(1<<uint(position)) == 0 {
			continue
		}
		value, err := decodeRWIColumn(reader, column)
		if err != nil {
			return RWIEntry{}, fmt.Errorf("%w: column %s: %w", ErrBadRWIEntry, column, err)
		}
		props[column] = value
	}
	if err := decodeRWIExtras(reader, props); err != nil {
		return RWIEntry{}, fmt.Errorf("%w: %w", ErrBadRWIEntry, err)
	}
	return RWIEntry{WordHash: wordHash, Properties: props}, nil
}

func decodeRWIColumn(reader *bytes.Reader, column string) (string, error) {
	switch column {
	case ColURLHash:
		raw := make([]byte, HashLength)
		if _, err := io.ReadFull(reader, raw); err != nil {
			return "", fmt.Errorf("%w: read hash: %w", ErrBadRWIEntry, err)
		}
		return string(raw), nil
	case ColLanguage:
		raw, err := readLengthPrefixed(reader)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	case ColFlags:
		raw, err := readLengthPrefixed(reader)
		if err != nil {
			return "", err
		}
		return Encode(raw), nil
	default:
		number, err := binary.ReadUvarint(reader)
		if err != nil {
			return "", fmt.Errorf("%w: read cardinal: %w", ErrBadRWIEntry, err)
		}
		return FormatRWICardinal(number), nil
	}
}

func decodeRWIExtras(reader *bytes.Reader, props map[string]string) error {
	count, err := binary.ReadUvarint(reader)
	if err != nil {
		return fmt.Errorf("%w: read extras count: %w", ErrBadRWIEntry, err)
	}
	if remaining := reader.Len(); remaining < 0 || count > uint64(remaining) {
		return fmt.Errorf("%w: extras count %d exceeds remaining bytes", ErrBadRWIEntry, count)
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
		return nil, fmt.Errorf("%w: read length: %w", ErrBadRWIEntry, err)
	}
	if remaining := reader.Len(); remaining < 0 || length > uint64(remaining) {
		return nil, fmt.Errorf("%w: length %d exceeds remaining bytes", ErrBadRWIEntry, length)
	}
	raw := make([]byte, length)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return nil, fmt.Errorf("%w: read bytes: %w", ErrBadRWIEntry, err)
	}
	return raw, nil
}
