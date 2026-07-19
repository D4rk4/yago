package compatibility

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCatalogCountsMatchDocumentation(t *testing.T) {
	want := map[State]int{}
	for _, count := range Catalog().Counts {
		want[count.State] = count.Total
	}

	root, err := os.OpenRoot(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("open project root: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close project root: %v", err)
		}
	})

	compatibilityPath := filepath.Join("yagonode", "doc", "compatibility.md")
	compatibilityTotals := compatibilityDocumentationTotals(t, root, compatibilityPath)
	if !equalStateTotals(compatibilityTotals, want) {
		t.Fatalf("compatibility documentation totals = %v, want %v", compatibilityTotals, want)
	}

	readmePath := "README.md"
	if got := readmeCompatibilityTotals(t, root, readmePath); !equalStateTotals(got, want) {
		t.Fatalf("README compatibility totals = %v, want %v", got, want)
	}
}

func compatibilityDocumentationTotals(t *testing.T, root *os.Root, path string) map[State]int {
	t.Helper()
	totals := map[State]int{}
	for _, fields := range markdownTableFields(t, root, path) {
		if len(fields) < 5 {
			continue
		}
		state := State(fields[4])
		if state == Implemented || state == Partial || state == Planned || state == Unsupported {
			totals[state]++
		}
	}

	return totals
}

func readmeCompatibilityTotals(t *testing.T, root *os.Root, path string) map[State]int {
	t.Helper()
	totals := map[State]int{}
	for _, fields := range markdownTableFields(t, root, path) {
		if len(fields) < 3 {
			continue
		}
		for _, state := range []State{Implemented, Partial, Planned, Unsupported} {
			if !strings.Contains(fields[1], string(state)) {
				continue
			}
			total, err := strconv.Atoi(fields[2])
			if err != nil {
				t.Fatalf("parse %s total %q: %v", state, fields[2], err)
			}
			totals[state] = total
		}
	}

	return totals
}

func markdownTableFields(t *testing.T, root *os.Root, path string) [][]string {
	t.Helper()
	file, err := root.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
	}()

	rows := make([][]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "|") {
			continue
		}
		fields := strings.Split(line, "|")
		for index := range fields {
			fields[index] = strings.TrimSpace(fields[index])
		}
		rows = append(rows, fields)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}

	return rows
}

func equalStateTotals(left, right map[State]int) bool {
	for _, state := range []State{Implemented, Partial, Planned, Unsupported} {
		if left[state] != right[state] {
			return false
		}
	}

	return true
}
