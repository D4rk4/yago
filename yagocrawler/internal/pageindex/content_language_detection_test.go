package pageindex

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
)

const artOfWarWindows1251 = "PGh0bWw+PGhlYWQ+PHRpdGxlPkFydE9mV2FyLiBBcnRPZldhcjogz+795+j/PC90aXRsZT48L2hlYWQ+PGJvZHk+PG1haW4+PGgxPkFydE9mV2FyOiDP7v3n6P88L2gxPjxwPsru4+TgIOLx5SDk8+z7IPLu6/zq7iDv7iDh++vu7PMg6CDi8OXs/ywg7+7i5fDt8+L46PH8IOLx7//y/Cwg5+Dx8vvr7iDt4CDi5fHzLCDx7+X48ywg4vHlIOHw7vHo4iwg6iDE8PPj8yDh7uXi7uzzLCDy7uzzLCDq8u4g4vHlIO/u6ezl8iwg/yDo8e/u4uXk/CDt5fHzLiDP7fHi//ng5fLx/yDh7uXi++wg5PDz5/z/7CDoIOHw4PL8/+wg7+4g5PP18y48L3A+PHA+w+7x8+Tg8PHy4u4g8fP55fHy4vPl8iDt5SDk6/8g8u7j7iwg9/Lu4fsg7eAg5+Xs6+Ug4fvrIPDg6Swg4CDk6/8g8u7j7iwg9/Lu4fsg7eAg5+Xs6+Ug7eUg4fvr7iDg5OAuPC9wPjwvbWFpbj48L2JvZHk+PC9odG1sPg=="

func TestIndexBuilderDetectsWindows1251RussianWithoutLanguageDeclaration(t *testing.T) {
	body, err := base64.StdEncoding.DecodeString(artOfWarWindows1251)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	page := pageparse.ParseHTML(
		"http://artofwar.ru/janr/index_janr_time7-1.shtml",
		"text/html; charset=windows-1251",
		body,
	)
	if page.Title != "ArtOfWar. ArtOfWar: Поэзия" || page.Language != "" {
		t.Fatalf("parsed page = %#v", page)
	}

	artifacts, err := NewIndexBuilder().Build(page, pageparse.BuildPageStats(page))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if artifacts.Document.Language != "ru" || artifacts.Metadata.Properties["lang"] != "ru" {
		t.Fatalf(
			"document language = %q, metadata language = %q",
			artifacts.Document.Language,
			artifacts.Metadata.Properties["lang"],
		)
	}
	for _, posting := range artifacts.Postings {
		if posting.Properties["l"] != "ru" {
			t.Fatalf("posting language = %q", posting.Properties["l"])
		}
	}
}

func TestResolveContentLanguageEvidenceOrder(t *testing.T) {
	if got := resolveContentLanguage(
		"This document contains enough ordinary English prose for reliable language identification across the extracted page body.",
		"ru",
	); got != "en" {
		t.Fatalf("reliable content language = %q", got)
	}
	if got := resolveContentLanguage("x", "de-DE"); got != "de" {
		t.Fatalf("declared language = %q", got)
	}
	if got := resolveContentLanguage("Привет", ""); got != "ru" {
		t.Fatalf("detected fallback language = %q", got)
	}
	if got := resolveContentLanguage("123", ""); got != "en" {
		t.Fatalf("signal-free language = %q", got)
	}
}

func TestLanguageEvidenceBoundsAtUTF8Boundary(t *testing.T) {
	short := "short"
	if got := languageEvidence(short); got != short {
		t.Fatalf("short evidence = %q", got)
	}
	text := strings.Repeat("a", maximumLanguageEvidenceBytes-1) + "é" + "tail"
	got := languageEvidence(text)
	if len(got) != maximumLanguageEvidenceBytes-1 || !strings.HasSuffix(got, "a") {
		t.Fatalf("bounded evidence length = %d", len(got))
	}
}
