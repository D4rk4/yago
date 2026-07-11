package contentsafety

import "testing"

func TestRecognizeStructuredLabels(t *testing.T) {
	trueValue := true
	falseValue := false
	tests := []struct {
		name   string
		labels StructuredLabels
		want   Evidence
	}{
		{name: "absent", want: Evidence{Rating: Unknown}},
		{
			name:   "adult",
			labels: StructuredLabels{RatingValues: []string{"  AdUlT  "}},
			want:   certainExplicitEvidence(),
		},
		{
			name:   "rta",
			labels: StructuredLabels{RatingValues: []string{RTARatingToken}},
			want:   certainExplicitEvidence(),
		},
		{
			name:   "family false",
			labels: StructuredLabels{FamilyFriendly: &falseValue},
			want:   certainExplicitEvidence(),
		},
		{
			name:   "family true",
			labels: StructuredLabels{FamilyFriendly: &trueValue},
			want:   Evidence{Rating: Unknown},
		},
		{
			name: "explicit wins conflict",
			labels: StructuredLabels{
				RatingValues:   []string{"adult"},
				FamilyFriendly: &trueValue,
			},
			want: certainExplicitEvidence(),
		},
		{
			name:   "unknown rating",
			labels: StructuredLabels{RatingValues: []string{"PG-13"}},
			want:   Evidence{Rating: Unknown},
		},
		{
			name:   "embedded rta",
			labels: StructuredLabels{RatingValues: []string{"prefix-" + RTARatingToken}},
			want:   Evidence{Rating: Unknown},
		},
		{
			name:   "lowercase rta",
			labels: StructuredLabels{RatingValues: []string{"rta-5042-1996-1400-1577-rta"}},
			want:   Evidence{Rating: Unknown},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := RecognizeStructured(test.labels); got != test.want {
				t.Fatalf("RecognizeStructured() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestProbabilityEvidence(t *testing.T) {
	general := probabilityEvidence(0.25)
	if general.Rating != General || general.ExplicitProbability != 0.25 ||
		general.Confidence != 0.5 {
		t.Fatalf("general probability evidence = %#v", general)
	}
	explicit := probabilityEvidence(0.75)
	if explicit.Rating != Explicit || explicit.ExplicitProbability != 0.75 ||
		explicit.Confidence != 0.5 {
		t.Fatalf("explicit probability evidence = %#v", explicit)
	}
}
