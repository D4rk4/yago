package yagonode

func normalizeStorageQuota(raw string) (string, error) {
	size, err := parseByteSize(raw)
	if err != nil {
		return "", err
	}

	return formatByteSize(size), nil
}
