package pagefetch

type BrowserRenderNeed func(FetchedPage) bool

func NewBrowserFallbackPageSource(
	primary PageSource,
	browser PageSource,
	renderNeeded BrowserRenderNeed,
) *FallbackPageSource {
	return &FallbackPageSource{
		primary:      primary,
		fallback:     browser,
		renderNeeded: renderNeeded,
	}
}
