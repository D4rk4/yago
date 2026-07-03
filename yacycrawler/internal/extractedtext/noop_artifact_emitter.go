package extractedtext

import (
	"context"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
)

type noopArtifactEmitter struct{}

func NewNoopArtifactEmitter() ArtifactEmitter {
	return noopArtifactEmitter{}
}

func (noopArtifactEmitter) Emit(context.Context, pageparse.ParsedPage, time.Time) error {
	return nil
}
