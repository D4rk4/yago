package tavilyapi

import "strings"

func boundedImageURLs(images []string) []string {
	bounded := make([]string, 0, min(len(images), maxResultImages))
	for _, image := range images {
		if len(bounded) >= maxResultImages {
			break
		}
		if strings.TrimSpace(image) != "" {
			bounded = append(bounded, image)
		}
	}

	return bounded
}

func retainedStringSliceBytes(values []string) int {
	size := len(values) * rawContentStringHeaderBytes
	for _, value := range values {
		size += len(value)
	}

	return size
}

func outputStringSliceBytes(values []string) int {
	size := 0
	for _, value := range values {
		size += rawContentJSONStringBytes(value)
	}

	return size
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	for index, value := range values {
		cloned[index] = strings.Clone(value)
	}

	return cloned
}
