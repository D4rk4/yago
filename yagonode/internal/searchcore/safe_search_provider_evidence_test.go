package searchcore

import "testing"

func TestSafeSearchAdmitsProviderFilteredEvidenceOnlyFromWeb(t *testing.T) {
	inner := stubSearcher{response: Response{TotalResults: 5, Results: []Result{
		{URL: "web-filtered", Source: SourceWeb, SafetyRating: SafetyProviderFiltered},
		{URL: "remote-filtered", Source: SourceRemote, SafetyRating: SafetyProviderFiltered},
		{URL: "local-filtered", Source: SourceLocal, SafetyRating: SafetyProviderFiltered},
		{URL: "web-unknown", Source: SourceWeb},
		{URL: "local-general", Source: SourceLocal, SafetyRating: SafetyGeneral},
	}}}

	response, err := NewSafeSearchSearcher(inner).Search(
		t.Context(),
		Request{SafeSearch: true, ContentDomain: ContentDomainText},
	)
	if err != nil {
		t.Fatal(err)
	}
	got := urls(response.Results)
	if len(got) != 2 || got[0] != "web-filtered" || got[1] != "local-general" ||
		response.TotalResults != 2 {
		t.Fatalf("response = %#v", response)
	}
}
