package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/snippetfetch"
)

func remoteTextEvidence(
	ctx context.Context,
	text string,
	terms []string,
	language string,
) (snippetfetch.TextEvidence, bool) {
	evidence, found := searchindex.FindTextQueryEvidence(ctx, text, terms, language)

	return snippetfetch.TextEvidence{Start: evidence.Start, End: evidence.End}, found
}
