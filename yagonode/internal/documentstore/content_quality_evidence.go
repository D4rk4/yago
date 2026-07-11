package documentstore

type ContentQualityEvidence struct {
	Known                bool
	Score                float64
	FunctionWordFraction float64
	SymbolFraction       float64
	AlphabeticFraction   float64
	UniqueTokenFraction  float64
	SpamRisk             float64
}
