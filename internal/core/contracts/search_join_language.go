package contracts

func (q SearchQuery) JoinLanguage() string {
	return ParseSearchModifier(q.Filters.Modifier).Language
}
