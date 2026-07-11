package yagonode

import (
	"context"
	"net/http"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/contentsafety"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type safetyClassifierFixture struct{}

func (safetyClassifierFixture) Classify(string) contentsafety.Evidence {
	return contentsafety.Evidence{Rating: contentsafety.General}
}

type safetyCrawlProcessFixture struct {
	classifier crawlresults.ContentSafetyClassifier
}

func (*safetyCrawlProcessFixture) mountDispatch(*http.ServeMux) {}
func (*safetyCrawlProcessFixture) Run(context.Context)          {}
func (*safetyCrawlProcessFixture) Close()                       {}

func (fixture *safetyCrawlProcessFixture) useContentSafetyClassifier(
	classifier crawlresults.ContentSafetyClassifier,
) {
	fixture.classifier = classifier
}

func TestAttachContentSafetyClassifier(t *testing.T) {
	classifier := safetyClassifierFixture{}
	fixture := &safetyCrawlProcessFixture{}
	attachContentSafetyClassifier(fixture, classifier)
	if fixture.classifier == nil {
		t.Fatal("classifier was not attached")
	}
	attachContentSafetyClassifier(nil, classifier)
	runtime := &crawlRuntime{consumer: crawlresults.NewIngestConsumer(nil, nil, nil, nil)}
	runtime.useContentSafetyClassifier(classifier)
}
