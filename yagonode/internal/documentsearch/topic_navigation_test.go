package documentsearch

import (
	"context"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func titled(title string) yagomodel.URIMetadataRow {
	return yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaColDescription: yagomodel.EncodeBase64WireForm(title),
	}}
}

func TestResultTopicsOrdersByFrequency(t *testing.T) {
	resources := []yagomodel.URIMetadataRow{
		titled("alpha beta gamma"),
		titled("alpha beta"),
		titled("alpha"),
	}

	got := resultTopics(context.Background(), resources, nil)
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("topics = %v, want %v", got, want)
	}
}

func TestResultTopicsExcludesQueryTerms(t *testing.T) {
	resources := []yagomodel.URIMetadataRow{
		titled("report budget review"),
		titled("budget review"),
	}
	queryTerms := []yagomodel.Hash{yagomodel.WordHash("budget")}

	got := resultTopics(context.Background(), resources, queryTerms)
	want := []string{"review", "report"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("topics = %v, want %v", got, want)
	}
}

func TestResultTopicsDropsShortAndNonLetters(t *testing.T) {
	resources := []yagomodel.URIMetadataRow{
		titled("go 2024 release notes"),
		titled("release notes"),
	}

	got := resultTopics(context.Background(), resources, nil)
	want := []string{"notes", "release"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("topics = %v, want %v", got, want)
	}
}

func TestResultTopicsCapsAtFive(t *testing.T) {
	resources := []yagomodel.URIMetadataRow{
		titled("one two three four five six seven"),
		titled("one two three four five six seven"),
	}

	got := resultTopics(context.Background(), resources, nil)
	if len(got) != maxTopics {
		t.Fatalf("topic count = %d, want %d", len(got), maxTopics)
	}
}

func TestResultTopicsReturnsSingleWord(t *testing.T) {
	resources := []yagomodel.URIMetadataRow{titled("alpha alpha alpha")}

	got := resultTopics(context.Background(), resources, nil)
	want := []string{"alpha"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("topics = %v, want %v", got, want)
	}
}

func TestResultTopicsDropsUnhelpfulWords(t *testing.T) {
	resources := []yagomodel.URIMetadataRow{
		titled("the alpha and beta"),
		titled("the alpha"),
	}

	got := resultTopics(context.Background(), resources, nil)
	want := []string{"alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("topics = %v, want %v", got, want)
	}
}

func TestResultTopicsSkipsUndecodableTitle(t *testing.T) {
	resources := []yagomodel.URIMetadataRow{
		{Properties: map[string]string{yagomodel.URLMetaColDescription: "z|@@@"}},
		titled("alpha beta"),
	}

	got := resultTopics(context.Background(), resources, nil)
	want := []string{"alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("topics = %v, want %v", got, want)
	}
}
