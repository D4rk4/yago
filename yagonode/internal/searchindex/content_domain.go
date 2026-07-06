package searchindex

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// mediaExtensions maps each media content domain to the file extensions that
// mark a document as belonging to it (the YaCy contentdom convention).
var mediaExtensions = map[string]map[string]bool{
	"audio": {
		"mp3": true, "ogg": true, "oga": true, "wav": true, "flac": true,
		"m4a": true, "aac": true, "opus": true,
	},
	"video": {
		"mp4": true, "webm": true, "mkv": true, "avi": true, "mov": true,
		"m4v": true, "mpg": true, "mpeg": true,
	},
	"app": {
		"apk": true, "exe": true, "msi": true, "dmg": true, "deb": true,
		"rpm": true, "jar": true, "appimage": true,
	},
}

// allowsContentDomain reports whether the document belongs to the requested
// content domain: image needs extracted images, the media domains match by the
// document's file extension, and text/all/empty accept everything.
func allowsContentDomain(doc documentstore.Document, domain string) bool {
	switch strings.ToLower(domain) {
	case "image":
		return len(doc.Images) > 0
	case "audio", "video", "app":
		return mediaExtensions[strings.ToLower(domain)][documentFileType(doc)]
	default:
		return true
	}
}
