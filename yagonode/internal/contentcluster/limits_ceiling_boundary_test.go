package contentcluster

import "testing"

// TestLimitsAdmitEveryHardCeilingUnchanged pins the accepting side of the
// configuration ceilings and their defaulting path together: a value sitting
// exactly on its hard maximum is a legal operator choice, and completion must
// hand it back untouched rather than substitute a default. Only one-past-the-
// ceiling is refused today, so narrowing any ceiling by one would keep every
// existing test green while making the largest documented configuration refuse
// to open.
func TestLimitsAdmitEveryHardCeilingUnchanged(t *testing.T) {
	ceiling := Limits{
		MaximumTextBytes:      hardMaximumTextBytes,
		MaximumShingles:       hardMaximumShingles,
		MaximumCandidates:     hardMaximumCandidates,
		MaximumBucketMembers:  hardMaximumBucketMembers,
		MaximumClusterMembers: hardMaximumClusterMembers,
		ShingleWords:          hardMaximumShingleWords,
		MinimumJaccard:        1,
	}

	completed, err := completeLimits(ceiling)
	if err != nil {
		t.Fatalf("ceiling limits error = %v", err)
	}
	if completed != ceiling {
		t.Fatalf("completed = %+v, want the ceiling unchanged %+v", completed, ceiling)
	}
}
