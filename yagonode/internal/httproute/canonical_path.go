package httproute

import "strings"

func CanonicalPath(value string) string {
	segments := make([]string, 0, strings.Count(value, "/"))
	for _, segment := range strings.Split(value, "/") {
		switch segment {
		case "", ".":
		case "..":
			if len(segments) != 0 {
				segments = segments[:len(segments)-1]
			}
		default:
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return "/"
	}

	return "/" + strings.Join(segments, "/")
}
