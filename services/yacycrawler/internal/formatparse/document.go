package formatparse

// Document is one parsed non-HTML document: the text a format parser
// extracted, with the title, links, and image references the format carries.
// It is deliberately format-neutral so a caller can map it onto whatever
// extraction result its pipeline uses.
type Document struct {
	URL             string
	Title           string
	Text            string
	Links           []string
	FollowableLinks []string
	Images          []ImageMetadata
}

// ImageMetadata is one image reference a document carries.
type ImageMetadata struct {
	URL     string
	AltText string
}

// Toggles switches the non-HTML format families on and off, the way YaCy's
// operator chooses which of TextParser's families a crawl may index.
type Toggles struct {
	// Text: txt, tex, csv, rtf, msg.
	Text bool
	// XMLFeeds: xml, rss, atom.
	XMLFeeds bool
	// PDF: pdf, ps.
	PDF bool
	// Office: OOXML, OpenDocument/StarOffice, legacy Office, Visio, FreeMind.
	Office bool
	// Images: jpg, png, gif, bmp, wbmp, tiff, psd, svg metadata.
	Images bool
	// Audio: mp3, ogg, wma, wav, m4a/m4b/m4p, mp4, aiff, ra/rm, sid tags.
	Audio bool
	// Misc: vcf, torrent, apk.
	Misc bool
	// Archives: zip, jar, epub, tar, gz/tgz, bz2/tbz/tbz2, xz/txz containers.
	Archives bool
}

// DefaultToggles enables every family except archive expansion, whose
// unpacking cost an operator opts into explicitly.
func DefaultToggles() Toggles {
	return Toggles{
		Text:     true,
		XMLFeeds: true,
		PDF:      true,
		Office:   true,
		Images:   true,
		Audio:    true,
		Misc:     true,
	}
}
