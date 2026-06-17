package yacyproto

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

// ErrBadField reports a request or response field that does not hold the value
// its endpoint requires.
var ErrBadField = errors.New("bad field")

func putString(dst url.Values, key, value string) {
	if value != "" {
		dst.Set(key, value)
	}
}

func putInt(dst url.Values, key string, value int) {
	dst.Set(key, strconv.Itoa(value))
}

func putIntOptional(dst url.Values, key string, value int) {
	if value != 0 {
		dst.Set(key, strconv.Itoa(value))
	}
}

func setString(dst yacymodel.Message, key, value string) {
	if value != "" {
		dst[key] = value
	}
}

func setInt(dst yacymodel.Message, key string, value int) {
	dst[key] = strconv.Itoa(value)
}

func readInt(key, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q", ErrBadField, key, value)
	}

	return n, nil
}

func optionalInt(key, value string) (int, error) {
	if value == "" {
		return 0, nil
	}

	return readInt(key, value)
}

func indexedKey(prefix string, i int) string {
	return prefix + strconv.Itoa(i)
}
