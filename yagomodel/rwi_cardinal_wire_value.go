package yagomodel

import "strconv"

func FormatRWICardinal(value uint64) string {
	return strconv.FormatUint(value, 10)
}
