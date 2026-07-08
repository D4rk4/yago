package crawlcapability

type ExtractedContent struct {
	Title                string
	Text                 string
	Language             string
	Links                []string
	LocalLinkCount       int
	ExternalLinkCount    int
	RefusesIndexing      bool
	RefusesLinkDiscovery bool
}
