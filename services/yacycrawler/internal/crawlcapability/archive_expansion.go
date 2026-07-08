package crawlcapability

import "errors"

var ErrContainerOverflow = errors.New("container overflow")

type ArchiveExpansion interface {
	Expand(containerURL, contentType string, body []byte) ([]ArchiveMember, error)
}
