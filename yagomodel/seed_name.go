package yagomodel

import "strings"

func NormalizeSeedName(name string) string {
	name = strings.ReplaceAll(name, "<", "_")
	name = strings.ReplaceAll(name, ">", "_")

	return strings.ToLower(name)
}
