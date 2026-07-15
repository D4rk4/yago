package searchindex

import "testing"

func TestStemWordKeepsChineseVocabularyWhole(t *testing.T) {
	if got := StemWord("搜索引擎"); got != "搜索引擎" {
		t.Fatalf("Chinese vocabulary stem = %q", got)
	}
	if got := StemWord("搜索引擎ＡＩ"); got != "搜索引擎ai" {
		t.Fatalf("normalized Chinese vocabulary stem = %q", got)
	}
	if got := StemWord("𝐀"); got != "A" {
		t.Fatalf("standard analyzer filter order = %q", got)
	}
}
