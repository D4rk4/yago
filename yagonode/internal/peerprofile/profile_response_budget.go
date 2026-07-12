package peerprofile

import "strings"

const maximumProfileResponseBytes = 2 << 20

func profileResponseSize(properties []Property) (int, bool) {
	if len(properties) > maximumProfileProperties {
		return 0, false
	}
	total := 0
	for _, property := range properties {
		key := sanitizedProfilePartSize(property.Key, maximumProfileResponseBytes-total)
		value := sanitizedProfilePartSize(property.Value, maximumProfileResponseBytes-total)
		if key == 0 || value == 0 {
			continue
		}
		entry := key + 1 + value + len(profileLineBreak)
		if entry > maximumProfileResponseBytes-total {
			return 0, false
		}
		total += entry
	}

	return total, true
}

func sanitizedProfilePartSize(value string, limit int) int {
	size := 0
	for index := 0; index < len(value); index++ {
		addition := 1
		switch value[index] {
		case '\r':
			addition = 0
		case '\n':
			addition = 2
		}
		if addition > limit-size {
			return limit + 1
		}
		size += addition
	}

	return size
}

func writeSanitizedProfilePart(destination *strings.Builder, value string) {
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\r':
		case '\n':
			destination.WriteString(`\n`)
		default:
			destination.WriteByte(value[index])
		}
	}
}
