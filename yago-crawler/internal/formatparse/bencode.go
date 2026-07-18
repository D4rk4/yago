package formatparse

import (
	"errors"
	"fmt"
	"strconv"
)

const bencodeMaxDepth = 32

var errBencode = errors.New("malformed bencode")

// decodeBencode reads one bencoded value at offset: integers become int64,
// strings become string, lists []any, and dictionaries map[string]any.
func decodeBencode(data []byte, depth int) (any, int, error) {
	if depth > bencodeMaxDepth || len(data) == 0 {
		return nil, 0, errBencode
	}
	switch {
	case data[0] == 'i':
		return decodeBencodeInt(data)
	case data[0] >= '0' && data[0] <= '9':
		return decodeBencodeString(data)
	case data[0] == 'l':
		return decodeBencodeList(data, depth)
	case data[0] == 'd':
		return decodeBencodeDict(data, depth)
	default:
		return nil, 0, errBencode
	}
}

func decodeBencodeInt(data []byte) (any, int, error) {
	for i := 1; i < len(data); i++ {
		if data[i] == 'e' {
			value, err := strconv.ParseInt(string(data[1:i]), 10, 64)
			if err != nil {
				return nil, 0, fmt.Errorf("bencode int: %w", err)
			}

			return value, i + 1, nil
		}
	}

	return nil, 0, errBencode
}

func decodeBencodeString(data []byte) (any, int, error) {
	for i := 0; i < len(data); i++ {
		if data[i] != ':' {
			continue
		}
		length, err := strconv.Atoi(string(data[:i]))
		if err != nil || length < 0 || i+1+length > len(data) {
			return nil, 0, errBencode
		}

		return string(data[i+1 : i+1+length]), i + 1 + length, nil
	}

	return nil, 0, errBencode
}

func decodeBencodeList(data []byte, depth int) (any, int, error) {
	out := make([]any, 0, 8)
	at := 1
	for at < len(data) && data[at] != 'e' {
		value, used, err := decodeBencode(data[at:], depth+1)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, value)
		at += used
	}
	if at >= len(data) {
		return nil, 0, errBencode
	}

	return out, at + 1, nil
}

func decodeBencodeDict(data []byte, depth int) (any, int, error) {
	out := make(map[string]any, 8)
	at := 1
	for at < len(data) && data[at] != 'e' {
		key, used, err := decodeBencodeString(data[at:])
		if err != nil {
			return nil, 0, err
		}
		at += used
		value, used, err := decodeBencode(data[at:], depth+1)
		if err != nil {
			return nil, 0, err
		}
		at += used
		name, _ := key.(string)
		out[name] = value
	}
	if at >= len(data) {
		return nil, 0, errBencode
	}

	return out, at + 1, nil
}
