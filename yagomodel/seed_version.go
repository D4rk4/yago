package yagomodel

type YaCyVersion string

func ParseYaCyVersion(s string) (YaCyVersion, error) {
	return YaCyVersion(s), nil
}

func (v YaCyVersion) String() string {
	return string(v)
}
