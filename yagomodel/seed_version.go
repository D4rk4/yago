package yagomodel

import (
	"fmt"
	"strconv"
)

type YaCyVersion string

func ParseYaCyVersion(s string) (YaCyVersion, error) {
	return YaCyVersion(s), nil
}

func (v YaCyVersion) String() string {
	return string(v)
}

// Float converts the seed version to YaCy's numeric form
// (Seed.getVersion uses Double.parseFloat on the wire value).
func (v YaCyVersion) Float() (float64, error) {
	value, err := strconv.ParseFloat(string(v), 64)
	if err != nil {
		return 0, fmt.Errorf("parse yacy version %q: %w", string(v), err)
	}

	return value, nil
}
