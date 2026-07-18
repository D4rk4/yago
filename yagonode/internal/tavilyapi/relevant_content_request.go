package tavilyapi

import "strings"

const (
	defaultRelevantChunks = 3
	maximumExtractChunks  = 5
)

func validateRelevantChunks(query string, chunks *int) error {
	if chunks == nil {
		return nil
	}
	if strings.TrimSpace(query) == "" {
		return badRequest("chunks_per_source requires query or instructions")
	}
	if *chunks < 1 || *chunks > maximumExtractChunks {
		return badRequest("chunks_per_source must be between 1 and 5")
	}

	return nil
}

func relevantChunkLimit(chunks *int) int {
	if chunks == nil {
		return defaultRelevantChunks
	}

	return *chunks
}

func requestedExtractContent(req ExtractRequest, text string) string {
	if strings.TrimSpace(req.Query) == "" {
		return text
	}

	return relevantChunks(text, answerTerms(req.Query), relevantChunkLimit(req.ChunksPerSource))
}

func requestedCrawlContent(req CrawlRequest, text string) string {
	if strings.TrimSpace(req.Instructions) == "" {
		return text
	}

	return relevantChunks(
		text,
		answerTerms(req.Instructions),
		relevantChunkLimit(req.ChunksPerSource),
	)
}
