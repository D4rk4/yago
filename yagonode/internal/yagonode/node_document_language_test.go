package yagonode

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
)

func TestProjectDocumentationUsesEnglishProse(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	rootDirectory, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open project root: %v", err)
	}
	t.Cleanup(func() {
		if err := rootDirectory.Close(); err != nil {
			t.Errorf("close project root: %v", err)
		}
	})
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk project documentation %q: %w", path, walkErr)
		}
		if entry.IsDir() {
			if projectDocumentationExcludedDirectory(entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		relativePath, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return fmt.Errorf("resolve project document %q: %w", path, relativeErr)
		}
		data, readErr := rootDirectory.ReadFile(relativePath)
		if readErr != nil {
			return fmt.Errorf("read project document %q: %w", path, readErr)
		}
		for _, line := range nonEnglishScriptMarkdownProseLines(string(data)) {
			t.Errorf("%s:%d contains non-English-script prose", path, line)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("scan project documentation: %v", err)
	}
}

func projectDocumentationExcludedDirectory(name string) bool {
	switch name {
	case ".git", ".toolchain", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func TestMarkdownProseLinesDistinguishesMultilingualExamples(t *testing.T) {
	document := strings.Join([]string{
		"English prose with `данные`.",
		"English prose with ``עוד ` بيانات``.",
		"```text",
		"精确 fixture",
		"```",
		"Русский prose is not allowed.",
		"中文 prose is not allowed.",
	}, "\n")
	if lines := nonEnglishScriptMarkdownProseLines(document); !slices.Equal(
		lines,
		[]int{6, 7},
	) {
		t.Fatalf("non-English-script prose lines = %v, want [6 7]", lines)
	}
}

func nonEnglishScriptMarkdownProseLines(document string) []int {
	lines := make([]int, 0)
	fence := ""
	for index, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if marker := markdownFenceMarker(trimmed); marker != "" {
			if fence == "" {
				fence = marker
			} else if marker == fence {
				fence = ""
			}

			continue
		}
		if fence != "" {
			continue
		}
		for _, character := range markdownWithoutInlineCode(line) {
			if unicode.IsLetter(character) &&
				!unicode.In(character, unicode.Latin, unicode.Greek) {
				lines = append(lines, index+1)

				break
			}
		}
	}

	return lines
}

func markdownFenceMarker(line string) string {
	if strings.HasPrefix(line, "```") {
		return "`"
	}
	if strings.HasPrefix(line, "~~~") {
		return "~"
	}

	return ""
}

func markdownWithoutInlineCode(line string) string {
	var prose strings.Builder
	for cursor := 0; cursor < len(line); {
		openingOffset := strings.IndexByte(line[cursor:], '`')
		if openingOffset < 0 {
			prose.WriteString(line[cursor:])

			break
		}
		opening := cursor + openingOffset
		prose.WriteString(line[cursor:opening])
		delimiterLength := 1
		for opening+delimiterLength < len(line) && line[opening+delimiterLength] == '`' {
			delimiterLength++
		}
		delimiter := strings.Repeat("`", delimiterLength)
		closingOffset := strings.Index(line[opening+delimiterLength:], delimiter)
		if closingOffset < 0 {
			prose.WriteString(line[opening:])

			break
		}
		cursor = opening + delimiterLength + closingOffset + delimiterLength
	}

	return prose.String()
}
