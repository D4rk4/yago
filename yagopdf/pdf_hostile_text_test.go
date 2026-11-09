package yagopdf

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
)

const (
	syntheticCMapPrologue = "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
	syntheticCMapEpilogue    = "endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
	hostileFillerEntryCount  = 100_000
	hostileFewFillerEntries  = 16
	hostileLateCodeOffset    = 0x0100
	hostileEarlyMappedText   = "Early mapped words"
	hostileLateMappedText    = "Latemappedwords"
	hostileIdentityASCIIText = "Identity mapped words"
)

func syntheticCMap(sections ...string) string {
	return syntheticCMapPrologue + strings.Join(sections, "") + syntheticCMapEpilogue
}

func bfcharSection(lines []string) string {
	return fmt.Sprintf("%d beginbfchar\n%s\nendbfchar\n", len(lines), strings.Join(lines, "\n"))
}

func bfrangeSection(lines []string) string {
	return fmt.Sprintf("%d beginbfrange\n%s\nendbfrange\n", len(lines), strings.Join(lines, "\n"))
}

func repeatedLines(line string, count int) []string {
	lines := make([]string, count)
	for index := range lines {
		lines[index] = line
	}

	return lines
}

func utf16BEHex(text string) string {
	var digits strings.Builder
	for _, unit := range utf16.Encode([]rune(text)) {
		fmt.Fprintf(&digits, "%04X", unit)
	}

	return digits.String()
}

func shiftedCodes(text string, offset rune) string {
	var codes strings.Builder
	for _, character := range text {
		fmt.Fprintf(&codes, "%04X", character+offset)
	}

	return codes.String()
}

func twoByteCodes(text string, offset rune) string {
	return "<" + shiftedCodes(text, offset) + ">"
}

func lateCodes(text string) string {
	return twoByteCodes(text, hostileLateCodeOffset-'A')
}

func identityCharacterLines(text string, offset rune) []string {
	seen := map[rune]bool{}
	lines := make([]string, 0, len(text))
	for _, character := range text {
		if seen[character] {
			continue
		}
		seen[character] = true
		lines = append(lines, fmt.Sprintf("<%04X> <%04X>", character+offset, character))
	}

	return lines
}

func toUnicodeFontPDF(cmap string, content string) []byte {
	return newSyntheticPDF().
		page(1, "/Resources << /Font << /F1 5 0 R >> >> /Contents 2 0 R").
		indirectObject(5, "<< /Type /Font /Subtype /Type0 /BaseFont /SyntheticSans /ToUnicode 6 0 R >>").
		flateStream(6, "", cmap).
		rawStream(2, "", content).
		finished()
}

func simpleEncodingFontPDF(
	encoding string,
	content string,
	extraObjects func(*syntheticPDF),
) []byte {
	pdf := newSyntheticPDF().
		page(1, "/Resources << /Font << /F1 5 0 R >> >> /Contents 2 0 R").
		indirectObject(5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding "+encoding+" >>").
		rawStream(2, "", content)
	extraObjects(pdf)

	return pdf.finished()
}

func informationTitlePDF(title string) []byte {
	return newSyntheticPDF().
		page(1, "/Contents 2 0 R").
		rawStream(2, "", "BT ("+hostileLegitimatePageText+") Tj ET").
		indirectObject(9, "<< /Title "+title+" >>").
		trailer("<< /Info 9 0 R >>").
		finished()
}

func TestHostileToUnicodeRangeOverTheWholeCodeSpaceMapsWithoutExpansion(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	content := "BT /F1 12 Tf " + twoByteCodes(hostileIdentityASCIIText, 0) + " Tj ET"
	for _, testCase := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"four byte range from zero", []string{"<00000000> <FFFFFFFF> <0000>"}, hostileIdentityASCIIText},
		{"two byte range from zero", []string{"<0000> <FFFF> <0000>"}, hostileIdentityASCIIText},
		{
			"thousands of full ranges", repeatedLines("<00000000> <FFFFFFFF> <0000>", hostileFillerEntryCount),
			hostileIdentityASCIIText,
		},
		{"full range starting at a surrogate", []string{"<0000> <FFFF> <D800>"}, ""},
		{"full range starting beyond the last code point", []string{"<0000> <FFFF> <DBFFDFFF>"}, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, toUnicodeFontPDF(
				syntheticCMap(bfrangeSection(testCase.lines)), content))
			if extraction.document.Text != testCase.want {
				t.Fatalf("full range text = %q, want %q", extraction.document.Text, testCase.want)
			}
		})
	}
}

func TestHostileToUnicodeEntriesBeyondTheEntryCapAreNotMapped(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	content := "BT /F1 12 Tf " + twoByteCodes(hostileEarlyMappedText, 0) + " Tj " +
		lateCodes(hostileLateMappedText) + " Tj ET"
	sections := map[string]func(fillers int) string{
		"bfchar": func(fillers int) string {
			lines := identityCharacterLines(hostileEarlyMappedText, 0)
			lines = append(lines, repeatedLines("<1000> <4E00>", fillers)...)
			lines = append(
				lines,
				identityCharacterLines(hostileLateMappedText, hostileLateCodeOffset-'A')...)

			return bfcharSection(lines)
		},
		"bfrange": func(fillers int) string {
			lines := make([]string, 0, fillers+4)
			lines = append(lines,
				"<0020> <0020> <0020>",
				"<0041> <005A> <0041>",
				"<0061> <007A> <0061>",
			)
			lines = append(lines, repeatedLines("<1000> <1000> <4E00>", fillers)...)
			lines = append(
				lines,
				fmt.Sprintf(
					"<%04X> <%04X> <0041>",
					hostileLateCodeOffset,
					hostileLateCodeOffset+0x39,
				),
			)

			return bfrangeSection(lines)
		},
	}
	for kind, section := range sections {
		for _, testCase := range []struct {
			fillers int
			late    bool
		}{
			{hostileFewFillerEntries, true},
			{pdfMaxCMapEntries, false},
			{hostileFillerEntryCount, false},
		} {
			t.Run(
				fmt.Sprintf("%s with %d filler entries", kind, testCase.fillers),
				func(t *testing.T) {
					parallelWhenAllocationIsUnmeasured(t)
					extraction := requireBoundedExtraction(t, toUnicodeFontPDF(
						syntheticCMap(section(testCase.fillers)), content))
					requireTextContains(t, extraction.document.Text, hostileEarlyMappedText)
					if got := strings.Contains(
						extraction.document.Text,
						hostileLateMappedText,
					); got != testCase.late {
						t.Fatalf(
							"late entry mapped = %t, want %t: %q",
							got,
							testCase.late,
							extraction.document.Text,
						)
					}
				},
			)
		}
	}
}

func TestHostileToUnicodeEntryCapIsSharedAcrossFonts(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name    string
		fillers int
		late    bool
	}{
		{"filler font below the shared cap", hostileFewFillerEntries, true},
		{"filler font exhausting the shared cap", pdfMaxCMapEntries, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			filler := syntheticCMap(
				bfrangeSection(repeatedLines("<1000> <1000> <4E00>", testCase.fillers)),
			)
			late := syntheticCMap(bfrangeSection([]string{
				fmt.Sprintf(
					"<%04X> <%04X> <0041>",
					hostileLateCodeOffset,
					hostileLateCodeOffset+0x39,
				),
			}))
			body := newSyntheticPDF().
				page(1, "/Resources << /Font << /F1 5 0 R /F2 7 0 R >> >> /Contents 2 0 R").
				indirectObject(5, "<< /Type /Font /Subtype /Type0 /ToUnicode 6 0 R >>").
				flateStream(6, "", filler).
				indirectObject(7, "<< /Type /Font /Subtype /Type0 /ToUnicode 8 0 R >>").
				flateStream(8, "", late).
				rawStream(2, "", "BT (Unselected font words) Tj /F2 12 Tf "+lateCodes(hostileLateMappedText)+" Tj ET").
				finished()
			extraction := requireBoundedExtraction(t, body)
			requireTextContains(t, extraction.document.Text, "Unselected font words")
			if got := strings.Contains(
				extraction.document.Text,
				hostileLateMappedText,
			); got != testCase.late {
				t.Fatalf(
					"second font mapped = %t, want %t: %q",
					got,
					testCase.late,
					extraction.document.Text,
				)
			}
		})
	}
}

func TestHostileToUnicodeArrayDestinationsAreSkipped(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	content := "BT /F1 12 Tf " + twoByteCodes("ARRAYMAPPED", 0) + " Tj " +
		twoByteCodes(" plain range words", 0) + " Tj ET"
	lowercase := []string{"<0020> <0020> <0020>", "<0061> <007A> <0061>"}
	letterArray := func(entries int) string {
		var array strings.Builder
		array.WriteString("<0041> <005A> [")
		for index := range entries {
			fmt.Fprintf(&array, "<%04X> ", 0x41+index%26)
		}
		array.WriteString("]")

		return array.String()
	}
	for _, testCase := range []struct {
		name      string
		uppercase string
		mapped    bool
	}{
		{"plain uppercase range", "<0041> <005A> <0041>", true},
		{"uppercase range with an array destination", letterArray(26), false},
		{"uppercase range with a two hundred thousand entry array", letterArray(200_000), false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, toUnicodeFontPDF(
				syntheticCMap(
					bfrangeSection(append([]string{testCase.uppercase}, lowercase...)),
				),
				content,
			))
			requireTextContains(t, extraction.document.Text, "plain range words")
			if got := strings.Contains(
				extraction.document.Text,
				"ARRAYMAPPED",
			); got != testCase.mapped {
				t.Fatalf(
					"uppercase mapped = %t, want %t: %q",
					got,
					testCase.mapped,
					extraction.document.Text,
				)
			}
		})
	}
}

func TestHostileToUnicodeLongDestinationRepeatedStopsAtTheOutputCap(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	longDestination := strings.Repeat("long destination words ", (64<<10)/23)
	cmap := syntheticCMap(
		bfcharSection([]string{"<0001> <" + utf16BEHex(longDestination) + ">"}),
		bfrangeSection([]string{"<0020> <007E> <0020>"}),
	)
	for _, testCase := range []struct {
		name        string
		repetitions int
		capped      bool
	}{
		{"one long destination", 1, false},
		{"long destination shown ten thousand times", 10_000, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			content := "BT /F1 12 Tf " + twoByteCodes(hostileIdentityASCIIText+" ", 0) + " Tj <" +
				strings.Repeat("0001", testCase.repetitions) + "> Tj ET"
			extraction := requireBoundedExtraction(t, toUnicodeFontPDF(cmap, content))
			if !strings.HasPrefix(extraction.document.Text, hostileIdentityASCIIText) {
				t.Fatalf("long destination text begins %q",
					extraction.document.Text[:min(len(extraction.document.Text), 64)])
			}
			if reachedCap := len(
				extraction.document.Text,
			) > pdfMaxTextBytes/2; reachedCap != testCase.capped {
				t.Fatalf("text bytes = %d, cap reached %t, want %t",
					len(extraction.document.Text), reachedCap, testCase.capped)
			}
		})
	}
}

func TestHostileDifferencesBeyondTheEntryCapAreIgnored(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	content := "BT /F1 12 Tf (BBBBBBBB remapped words) Tj ET"
	for _, testCase := range []struct {
		name    string
		repeats int
		want    string
	}{
		{"remapping within the entry cap", 10, "ZZZZZZZZ remapped words"},
		{"remapping after the entry cap", 300, "BBBBBBBB remapped words"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			encoding := "<< /BaseEncoding /WinAnsiEncoding /Differences [" +
				strings.Repeat("65 /A ", testCase.repeats) + "66 /Z] >>"
			extraction := requireBoundedExtraction(
				t,
				simpleEncodingFontPDF(encoding, content, func(*syntheticPDF) {}),
			)
			if extraction.document.Text != testCase.want {
				t.Fatalf("differences text = %q, want %q", extraction.document.Text, testCase.want)
			}
		})
	}
}

func TestHostileDifferencesBeyondTheEncodingDictionaryCapLeaveTheFontUnavailable(t *testing.T) {
	skipWhenAllocationIsUnmeasured(t)
	content := "BT (Unselected font words) Tj /F1 12 Tf (Selected font words) Tj ET"
	differences := func(entries int) string {
		return "<< /BaseEncoding /WinAnsiEncoding /Differences [" + strings.Repeat(
			"1 /bullet ",
			entries,
		) + "] >>"
	}
	for _, testCase := range []struct {
		name     string
		entries  int
		indirect bool
		selected bool
	}{
		{"hundred inline entries", 100, false, true},
		{"hundred thousand inline entries", hostileFillerEntryCount, false, false},
		{"hundred indirect entries", 100, true, true},
		{"hundred thousand indirect entries", hostileFillerEntryCount, true, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			encoding := differences(testCase.entries)
			extraObjects := func(*syntheticPDF) {}
			if testCase.indirect {
				indirectEncoding := encoding
				extraObjects = func(pdf *syntheticPDF) { pdf.indirectObject(7, indirectEncoding) }
				encoding = "7 0 R"
			}
			extraction := requireBoundedExtraction(
				t,
				simpleEncodingFontPDF(encoding, content, extraObjects),
			)
			requireTextContains(t, extraction.document.Text, "Unselected font words")
			if got := strings.Contains(
				extraction.document.Text,
				"Selected font words",
			); got != testCase.selected {
				t.Fatalf(
					"selected font decoded = %t, want %t: %q",
					got,
					testCase.selected,
					extraction.document.Text,
				)
			}
		})
	}
}

func TestHostileTenMillionShowOperatorsStopAtTheOutputCapOnARuneBoundary(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	const showOperators = 10_000_000
	for _, testCase := range []struct {
		name      string
		pattern   string
		operators int
	}{
		{"ascii show operators", "BT (a) Tj (b) ' (c) \" [(d) -250 (e)] TJ ET\n", 4},
		{"multibyte show operators", "BT (\\351t\\351) Tj <FEFFD83DDE00> Tj ET\n", 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			naiveBytes := showOperators / testCase.operators * len(testCase.pattern)
			body := newSyntheticPDF().
				page(1, "/Contents [2 0 R 3 0 R]").
				rawStream(2, "", "BT ("+hostileLegitimatePageText+") Tj ET").
				encodedStream(3, "/Filter /FlateDecode", syntheticRepeatedZlib([]byte(testCase.pattern), naiveBytes)).
				finished()
			extraction := requireBoundedExtraction(t, body)
			if !strings.HasPrefix(extraction.document.Text, hostileLegitimatePageText) {
				t.Fatalf(
					"show operator text begins %q",
					extraction.document.Text[:min(len(extraction.document.Text), 64)],
				)
			}
			if len(extraction.document.Text) < pdfMaxTextBytes/2 {
				t.Fatalf(
					"show operator text stopped at %d bytes, before the output cap",
					len(extraction.document.Text),
				)
			}
		})
	}
}

func TestHostileInformationTitleStaysWithinTheTitleCap(t *testing.T) {
	skipWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name   string
		title  string
		want   string
		capped bool
	}{
		{"short literal title", "(Synthetic harbour guide)", "Synthetic harbour guide", false},
		{"one mebibyte literal title", "(" + strings.Repeat("Title words ", (1<<20)/12) + ")", "Title words", true},
		{"one mebibyte hex title", "<" + strings.Repeat("5469746C6520", (1<<20)/6) + ">", "Title", true},
		{"one mebibyte four byte rune title", "<FEFF" + strings.Repeat("D83DDE00", (1<<20)/4) + ">", "\U0001F600", true},
		{"unterminated literal title", "(" + strings.Repeat("Open title ", (1<<20)/11), "", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, informationTitlePDF(testCase.title))
			if extraction.document.Text != hostileLegitimatePageText {
				t.Fatalf("page text = %q", extraction.document.Text)
			}
			title := extraction.document.Title
			if !strings.HasPrefix(title, testCase.want) || testCase.want == "" && title != "" {
				t.Fatalf("title begins %q, want %q", title[:min(len(title), 64)], testCase.want)
			}
			if reachedCap := len(title) > pdfMaxTitleBytes/2; reachedCap != testCase.capped {
				t.Fatalf(
					"title bytes = %d, cap reached %t, want %t",
					len(title),
					reachedCap,
					testCase.capped,
				)
			}
		})
	}
}

func TestHostileShownStringsDropC0ControlsTurnLiteralLineBreaksIntoSpacesAndStayValidUTF8(
	t *testing.T,
) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name  string
		shown string
		want  string
	}{
		{"octal escaped controls in a literal", `(Control\001\002\033\177 bytes stay out)`, "Control bytes stay out"},
		{
			"octal escaped line breaks in a literal", `(Line\012feed\015return\011tab words)`,
			"Line feed return tab words",
		},
		{"raw controls in a literal", "(Raw\x01\x02\x07\x1b control bytes)", "Raw control bytes"},
		{
			"high bytes read as single byte text", `(Caf\351 \303\251 \377\376 bytes)`,
			"Caf\u00E9 \u00C3\u00A9 \u00FF\u00FE bytes",
		},
		{
			"invalid utf-8 sequences read as single byte text", "(Invalid \xc3A \xed\xa0\x80 \xf8\x88 bytes)",
			"Invalid \u00C3A \u00ED \u00F8 bytes",
		},
		{
			"utf-16 controls", "<FEFF" + utf16BEHex("Sixteen\x00\x07\x1b bit controls") + ">",
			"Sixteen bit controls",
		},
		{"utf-16 unpaired surrogates", "<FEFF" + utf16BEHex("Unpaired ") + "D800" + utf16BEHex("high ") +
			"DC00" + utf16BEHex("low surrogate words") + ">", "Unpaired \uFFFDhigh \uFFFDlow surrogate words"},
		{
			"utf-16 paired surrogates", "<FEFF" + utf16BEHex("Paired \U0001F600 surrogate words") + ">",
			"Paired \U0001F600 surrogate words",
		},
		{
			"utf-16 noncharacters and bidi controls",
			"<FEFF" + utf16BEHex("Non\uFFFE\uFFFF\uFDD0character \u202Ebidi\u200F words") + ">",
			"Noncharacter bidi words",
		},
		{
			"utf-16 ligature", "<FEFF" + utf16BEHex("Ligature \uFB01ne \uFB00ect words") + ">",
			"Ligature fine ffect words",
		},
		{"utf-16 odd trailing byte", "<FEFF" + utf16BEHex("Odd length words") + "41>", "Odd length words"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, singlePagePDF("BT "+testCase.shown+" Tj ET"))
			if !extraction.readable || extraction.document.Text != testCase.want {
				t.Fatalf(
					"shown string text = %t %q, want %q",
					extraction.readable,
					extraction.document.Text,
					testCase.want,
				)
			}
		})
	}
}

func TestHostileUTF16LineFeedsAreKeptWhileTabsAndCarriageReturnsBecomeSpaces(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name  string
		shown string
		want  string
	}{
		{
			"line feed", "<FEFF" + utf16BEHex("First sixteen line\nSecond sixteen line") + ">",
			"First sixteen line\nSecond sixteen line",
		},
		{"carriage return", "<FEFF" + utf16BEHex("First sixteen\rreturn words") + ">", "First sixteen return words"},
		{"tab", "<FEFF" + utf16BEHex("First sixteen\ttab words") + ">", "First sixteen tab words"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, singlePagePDF("BT "+testCase.shown+" Tj ET"))
			if extraction.document.Text != testCase.want {
				t.Fatalf("utf-16 text = %q, want %q", extraction.document.Text, testCase.want)
			}
		})
	}
}

func TestHostileToUnicodeDestinationsDropC0ControlsKeepLineFeedsAndRefuseSurrogates(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	cmap := syntheticCMap(
		bfcharSection([]string{
			"<0001> <0000>", "<0002> <0007>", "<0003> <000A>", "<0004> <0009>", "<0005> <FFFE>",
			"<0006> <D800>", "<0007> <DC00>", "<0008> <D83DDE00>", "<0009> <202E>", "<000A> <FB01>",
			"<000B> <000D>", "<000C> <001B>", "<000D> <0000004100000042>",
		}),
		bfrangeSection(
			[]string{"<0010> <0012> <D7FE>", "<0013> <0017> <DBFFDFFD>", "<0020> <007E> <0020>"},
		),
	)
	text := func(value string) string { return shiftedCodes(value, 0) }
	for _, testCase := range []struct {
		name  string
		codes string
		want  string
	}{
		{
			"nul bell and escape destinations",
			text("C") + "0001" + text("ontr") + "0002" + text("olled") + "000C" + text(" words"),
			"Controlled words",
		},
		{
			"line feed destination", text("Mapped first line") + "0003" + text("mapped second line"),
			"Mapped first line\nmapped second line",
		},
		{
			"tab and carriage return destinations", text("Mapped") + "0004" + text("tab") + "000B" + text("return words"),
			"Mapped tab return words",
		},
		{
			"noncharacter and bidi destinations", text("Mapped ") + "00050009" + text("hidden words"),
			"Mapped hidden words",
		},
		{
			"surrogate destinations", text("Mapped ") + "00060007" + text("surrogate words"),
			"Mapped surrogate words",
		},
		{
			"paired surrogate and ligature destinations", text("Mapped ") + "0008" + text(" ") + "000A" + text("ne words"),
			"Mapped \U0001F600 fine words",
		},
		{"embedded nul destinations", text("Mapped ") + "000D" + text(" embedded words"), "Mapped AB embedded words"},
		{
			"range crossing into surrogates", text("Range ") + "001000110012" + text(" crossing words"),
			"Range \uD7FE\uD7FF crossing words",
		},
		{"range crossing noncharacters and the last code point", text("Range ") + "00130014001500160017" +
			text(" ending words"), "Range \U0010FFFD ending words"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, toUnicodeFontPDF(cmap,
				"BT /F1 12 Tf <"+testCase.codes+"> Tj ET"))
			if extraction.document.Text != testCase.want {
				t.Fatalf("destination text = %q, want %q", extraction.document.Text, testCase.want)
			}
		})
	}
}

func TestHostileInformationTitleDropsC0ControlsAndStaysValidUTF8(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	for _, testCase := range []struct {
		name  string
		title string
		want  string
	}{
		{"literal controls", `(Title\001\002\033 with\012line\011breaks)`, "Title with line breaks"},
		{"literal high bytes", `(Caf\351 \377 title)`, "Caf\u00E9 \u00FF title"},
		{"utf-16 controls and surrogates", "<FEFF" + utf16BEHex("Sixteen\x00\x07") + "D800" +
			utf16BEHex(" bit\n title") + ">", "Sixteen\uFFFD bit title"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			extraction := requireBoundedExtraction(t, informationTitlePDF(testCase.title))
			if extraction.document.Title != testCase.want {
				t.Fatalf("title = %q, want %q", extraction.document.Title, testCase.want)
			}
		})
	}
}
