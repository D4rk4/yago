package pageindex

import "strconv"

func componentCardinal(count int) string {
	var value uint64
	if count < 0 {
		value = 0
	} else {
		value = uint64(count) & 0xff
	}
	return strconv.FormatUint(value, 10)
}
