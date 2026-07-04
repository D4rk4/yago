package yagomodel

import (
	"fmt"
	"strconv"
)

func (e RWIPosting) Cardinal(key string) (uint64, error) {
	value := e.Properties[key]
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse rwi cardinal %s: %w", key, err)
	}
	return n, nil
}

func (e RWIPosting) ByteValue(key string) (byte, error) {
	value := e.Properties[key]
	n, err := strconv.ParseUint(value, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("parse rwi byte %s: %w", key, err)
	}
	return byte(n), nil
}
