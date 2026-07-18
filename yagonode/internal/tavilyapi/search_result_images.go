package tavilyapi

import "strings"

func retainedSearchResultImages(images any) (int, int) {
	switch values := images.(type) {
	case []string:
		retained := len(values) * rawContentStringHeaderBytes
		output := 0
		for _, value := range values {
			retained += len(value)
			output += rawContentJSONStringBytes(value)
		}

		return retained, output
	case []SearchImage:
		retained := len(values) * rawContentSearchImageBytes
		output := len(values) * rawContentResultJSONBytes
		for _, value := range values {
			retained += len(value.URL) + len(value.Description)
			output += rawContentJSONStringBytes(value.URL) +
				rawContentJSONStringBytes(value.Description)
		}

		return retained, output
	default:
		return 0, 0
	}
}

func cloneSearchResultImages(images any) any {
	switch values := images.(type) {
	case []string:
		cloned := make([]string, len(values))
		for index, value := range values {
			cloned[index] = strings.Clone(value)
		}

		return cloned
	case []SearchImage:
		cloned := make([]SearchImage, len(values))
		for index, value := range values {
			cloned[index] = SearchImage{
				URL:         strings.Clone(value.URL),
				Description: strings.Clone(value.Description),
			}
		}

		return cloned
	default:
		return nil
	}
}
