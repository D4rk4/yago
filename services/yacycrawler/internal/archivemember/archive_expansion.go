package archivemember

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

const (
	mediaZip  = "application/zip"
	mediaTar  = "application/x-tar"
	mediaGzip = "application/gzip"

	memberSeparator = "!/"
)

type ArchiveExpansion struct {
	maxMembers     int
	maxMemberBytes int64
}

func New(maxMembers int, maxMemberBytes int64) ArchiveExpansion {
	return ArchiveExpansion{maxMembers: maxMembers, maxMemberBytes: maxMemberBytes}
}

func (ArchiveExpansion) MediaTypes() []string {
	return []string{mediaZip, mediaTar, mediaGzip}
}

func (a ArchiveExpansion) Expand(
	containerURL, contentType string,
	body []byte,
) ([]crawlcapability.ArchiveMember, error) {
	switch mediaType(contentType) {
	case mediaZip:
		return a.expandZip(containerURL, body)
	case mediaTar:
		return a.expandTar(containerURL, bytes.NewReader(body))
	case mediaGzip:
		return a.expandGzip(containerURL, body)
	default:
		return nil, crawlcapability.ErrUnsupportedMediaType
	}
}

func (a ArchiveExpansion) expandZip(
	containerURL string,
	body []byte,
) ([]crawlcapability.ArchiveMember, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	var members []crawlcapability.ArchiveMember
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			continue
		}
		content, read := a.readMember(opened)
		_ = opened.Close()
		if !read {
			continue
		}
		member, ok := a.member(containerURL, file.Name, content)
		if !ok {
			continue
		}
		members = append(members, member)
		if len(members) > a.maxMembers {
			return nil, crawlcapability.ErrContainerOverflow
		}
	}
	return members, nil
}

func (a ArchiveExpansion) expandGzip(
	containerURL string,
	body []byte,
) ([]crawlcapability.ArchiveMember, error) {
	decompressed, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	content, read := a.readMember(decompressed)
	if !read {
		return nil, nil
	}
	member, ok := a.member(containerURL, decompressedName(containerURL), content)
	if !ok {
		return nil, nil
	}
	return []crawlcapability.ArchiveMember{member}, nil
}

func decompressedName(containerURL string) string {
	return strings.TrimSuffix(path.Base(containerURL), path.Ext(containerURL))
}

func (a ArchiveExpansion) expandTar(
	containerURL string,
	source io.Reader,
) ([]crawlcapability.ArchiveMember, error) {
	reader := tar.NewReader(source)
	var members []crawlcapability.ArchiveMember
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		content, read := a.readMember(reader)
		if !read {
			continue
		}
		member, ok := a.member(containerURL, header.Name, content)
		if !ok {
			continue
		}
		members = append(members, member)
		if len(members) > a.maxMembers {
			return nil, crawlcapability.ErrContainerOverflow
		}
	}
	return members, nil
}

func (a ArchiveExpansion) readMember(source io.Reader) ([]byte, bool) {
	content, err := io.ReadAll(io.LimitReader(source, a.maxMemberBytes+1))
	if err != nil {
		return nil, false
	}
	if int64(len(content)) > a.maxMemberBytes {
		return nil, false
	}
	return content, true
}

func (a ArchiveExpansion) member(
	containerURL, name string,
	content []byte,
) (crawlcapability.ArchiveMember, bool) {
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		return crawlcapability.ArchiveMember{}, false
	}
	return crawlcapability.ArchiveMember{
		URL:         containerURL + memberSeparator + name,
		ContentType: contentType,
		Body:        content,
	}, true
}

func mediaType(contentType string) string {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	}
	return media
}
