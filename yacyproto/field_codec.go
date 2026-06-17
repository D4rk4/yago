package yacyproto

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

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

func putBoolOptional(dst url.Values, key string, value bool) {
	if value {
		dst.Set(key, strconv.FormatBool(value))
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

func optionalBool(key, value string) (bool, error) {
	if value == "" {
		return false, nil
	}

	switch strings.ToLower(value) {
	case "true", "on", "1":
		return true, nil
	case "false", "off", "0":
		return false, nil
	default:
		return false, fmt.Errorf("%w: %s=%q", ErrBadField, key, value)
	}
}

func indexedKey(prefix string, i int) string {
	return prefix + strconv.Itoa(i)
}
