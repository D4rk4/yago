package sitehost

import "testing"

func TestMatchesExactBareAndWWWCounterpart(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		constraint string
	}{
		{name: "exact bare", host: "example.org", constraint: "example.org"},
		{name: "exact www", host: "www.example.org", constraint: "www.example.org"},
		{name: "bare query finds www", host: "www.example.org", constraint: "example.org"},
		{name: "www query finds bare", host: "example.org", constraint: "www.example.org"},
		{
			name:       "nested bare query finds www",
			host:       "www.docs.example.org",
			constraint: "docs.example.org",
		},
		{
			name:       "nested www query finds bare",
			host:       "docs.example.org",
			constraint: "www.docs.example.org",
		},
		{
			name:       "case and terminal root label",
			host:       "WWW.Example.ORG.",
			constraint: ".example.org.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !Matches(test.host, test.constraint) {
				t.Fatalf("Matches(%q, %q) = false", test.host, test.constraint)
			}
		})
	}
}

func TestRejectsSiblingAndBoundaryConfusion(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		constraint string
	}{
		{name: "arbitrary subdomain", host: "docs.example.org", constraint: "example.org"},
		{name: "sibling of www", host: "docs.example.org", constraint: "www.example.org"},
		{name: "parent is not child", host: "example.org", constraint: "docs.example.org"},
		{name: "prefixed label", host: "evilwww.example.org", constraint: "www.example.org"},
		{name: "hyphen boundary", host: "evil-example.org", constraint: "example.org"},
		{name: "suffix attack", host: "example.org.evil", constraint: "example.org"},
		{name: "www suffix attack", host: "www.example.org.evil", constraint: "www.example.org"},
		{name: "empty host", host: "", constraint: "example.org"},
		{name: "empty constraint", host: "example.org", constraint: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if Matches(test.host, test.constraint) {
				t.Fatalf("Matches(%q, %q) = true", test.host, test.constraint)
			}
		})
	}
}

func TestEquivalentsPreserveConstraintThenCounterpart(t *testing.T) {
	tests := []struct {
		constraint string
		want       []string
	}{
		{constraint: "Example.ORG.", want: []string{"example.org", "www.example.org"}},
		{constraint: "www.Example.ORG", want: []string{"www.example.org", "example.org"}},
		{constraint: ".", want: nil},
	}
	for _, test := range tests {
		got := Equivalents(test.constraint)
		if len(got) != len(test.want) {
			t.Fatalf("Equivalents(%q) = %v, want %v", test.constraint, got, test.want)
		}
		for index := range test.want {
			if got[index] != test.want[index] {
				t.Fatalf("Equivalents(%q) = %v, want %v", test.constraint, got, test.want)
			}
		}
	}
}
