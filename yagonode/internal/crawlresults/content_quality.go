package crawlresults

import (
	"github.com/D4rk4/yago/yagonode/internal/contentprior"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func contentQualityFromText(text string) documentstore.ContentQualityEvidence {
	evidence := contentprior.Analyze(text)
	return documentstore.ContentQualityEvidence{
		Known:                evidence.Known,
		Score:                evidence.Score,
		FunctionWordFraction: evidence.FunctionWordFraction,
		SymbolFraction:       evidence.SymbolFraction,
		AlphabeticFraction:   evidence.AlphabeticFraction,
		UniqueTokenFraction:  evidence.UniqueTokenFraction,
		SpamRisk:             evidence.SpamRisk,
	}
}
