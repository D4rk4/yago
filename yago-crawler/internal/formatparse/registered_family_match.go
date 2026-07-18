package formatparse

func registeredFamilyMIME(mime string) bool {
	for _, entry := range families() {
		if entry.mimes[mime] {
			return true
		}
	}

	return false
}
