package ingest

import "sort"

func encodedPropertiesSize(properties map[string]string) int {
	if len(properties) == 0 {
		return 0
	}

	return encodedSize(properties)
}

func halveProperties(properties map[string]string) map[string]string {
	if len(properties) <= 1 {
		return nil
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	keys = keys[:len(keys)/2]
	halved := make(map[string]string, len(keys))
	for _, key := range keys {
		halved[key] = properties[key]
	}

	return halved
}
