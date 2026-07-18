package searchindex

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/analysis/lang/cjk"
	bleveunicode "github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/blevesearch/vellum"
)

const (
	cjkChineseTextAnalyzer    = "cjk_zh"
	cjkJapaneseTextAnalyzer   = "cjk_ja"
	cjkKoreanTextAnalyzer     = "cjk_ko"
	cjkChineseQueryAnalyzer   = "cjk_zh_query"
	cjkJapaneseQueryAnalyzer  = "cjk_ja_query"
	cjkKoreanQueryAnalyzer    = "cjk_ko_query"
	cjkDictionaryComponent    = "yago_cjk_dictionary"
	cjkChineseComponent       = "yago_cjk_zh"
	cjkJapaneseComponent      = "yago_cjk_ja"
	cjkKoreanComponent        = "yago_cjk_ko"
	cjkChineseQueryComponent  = "yago_cjk_zh_query"
	cjkJapaneseQueryComponent = "yago_cjk_ja_query"
	cjkKoreanQueryComponent   = "yago_cjk_ko_query"
	cjkMaximumWordCharacters  = 24
)

type cjkDictionaryTokenizerConfiguration struct {
	canonicalize bool
	splitHangul  bool
	dictionary   func() (*vellum.FST, error)
}

type cjkDictionaryTokenizerInstance struct {
	base           analysis.Tokenizer
	canonicalize   bool
	splitHangul    bool
	dictionary     func() (*vellum.FST, error)
	widthNormalize analysis.TokenFilter
}

type cjkTextUnit struct {
	term  string
	start int
	end   int
}

var errCJKTokenizerRegistration = registry.RegisterTokenizer(
	cjkDictionaryComponent,
	newCJKDictionaryTokenizer,
)

func newCJKDictionaryTokenizer(
	config map[string]any,
	_ *registry.Cache,
) (analysis.Tokenizer, error) {
	language, ok := config["language"].(string)
	if !ok {
		return nil, fmt.Errorf("CJK tokenizer language required")
	}
	withDictionary, ok := config["dictionary"].(bool)
	if !ok {
		return nil, fmt.Errorf("CJK tokenizer dictionary mode required")
	}
	configuration, found := cjkTokenizerConfiguration(language, withDictionary)
	if !found {
		return nil, fmt.Errorf("unsupported CJK tokenizer language %q", language)
	}

	return &cjkDictionaryTokenizerInstance{
		base:           bleveunicode.NewUnicodeTokenizer(),
		canonicalize:   configuration.canonicalize,
		splitHangul:    configuration.splitHangul,
		dictionary:     configuration.dictionary,
		widthNormalize: cjk.NewCJKWidthFilter(),
	}, nil
}

func cjkTokenizerConfiguration(
	language string,
	withDictionary bool,
) (cjkDictionaryTokenizerConfiguration, bool) {
	switch language {
	case "zh":
		configuration := cjkDictionaryTokenizerConfiguration{canonicalize: true}
		if withDictionary {
			configuration.dictionary = func() (*vellum.FST, error) {
				dictionary, err := loadCJKChineseDictionary()
				if err != nil {
					return nil, err
				}

				return dictionary.lexicon, nil
			}
		}

		return configuration, true
	case "ja":
		configuration := cjkDictionaryTokenizerConfiguration{}
		if withDictionary {
			configuration.dictionary = loadCJKJapaneseDictionary
		}

		return configuration, true
	case "ko":
		return cjkDictionaryTokenizerConfiguration{splitHangul: true}, true
	default:
		return cjkDictionaryTokenizerConfiguration{}, false
	}
}

func (t *cjkDictionaryTokenizerInstance) Tokenize(input []byte) analysis.TokenStream {
	base := t.base.Tokenize(input)
	output := make(analysis.TokenStream, 0, len(base)*2)
	position := 1
	for index := 0; index < len(base); {
		token := base[index]
		if !t.sequenceToken(token, input) {
			copy := *token
			copy.Position = position
			output = append(output, &copy)
			position++
			index++

			continue
		}
		end := index + 1
		for end < len(base) && t.sequenceToken(base[end], input) &&
			base[end-1].End == base[end].Start {
			end++
		}
		units := t.textUnits(input, token.Start, base[end-1].End)
		if t.canonicalize {
			units = canonicalChineseUnits(units)
		}
		var dictionary *vellum.FST
		if t.dictionary != nil {
			dictionary, _ = t.dictionary()
		}
		output = append(output, dictionaryCJKTokens(units, position, dictionary)...)
		position += len(units)
		index = end
	}

	return output
}

func (t *cjkDictionaryTokenizerInstance) sequenceToken(
	token *analysis.Token,
	input []byte,
) bool {
	if token.Type == analysis.Ideographic {
		return true
	}
	if !t.splitHangul {
		return false
	}
	for _, character := range string(input[token.Start:token.End]) {
		if !unicode.Is(unicode.Hangul, character) {
			return false
		}
	}

	return token.End > token.Start
}

func (t *cjkDictionaryTokenizerInstance) textUnits(
	input []byte,
	start int,
	end int,
) []cjkTextUnit {
	units := make([]cjkTextUnit, 0, utf8.RuneCount(input[start:end]))
	for offset := start; offset < end; {
		_, size := utf8.DecodeRune(input[offset:end])
		unitEnd := offset + size
		if unitEnd < end {
			next, nextSize := utf8.DecodeRune(input[unitEnd:end])
			if next == '\uFF9E' || next == '\uFF9F' {
				unitEnd += nextSize
			}
		}
		token := &analysis.Token{Term: append([]byte(nil), input[offset:unitEnd]...)}
		normalized := t.widthNormalize.Filter(analysis.TokenStream{token})
		units = append(units, cjkTextUnit{
			term:  string(normalized[0].Term),
			start: offset,
			end:   unitEnd,
		})
		offset = unitEnd
	}

	return units
}

func canonicalChineseUnits(units []cjkTextUnit) []cjkTextUnit {
	dictionary, err := loadCJKChineseDictionary()
	if err != nil {
		return units
	}
	for start := 0; start < len(units); {
		end, output, found := longestCJKConversion(
			dictionary,
			units,
			start,
		)
		if !found {
			start++

			continue
		}
		converted := []rune(output)
		for index := range converted {
			units[start+index].term = string(converted[index])
		}
		start = end
	}

	return units
}

func longestCJKConversion(
	dictionary *cjkChineseDictionary,
	units []cjkTextUnit,
	start int,
) (int, string, bool) {
	state := dictionary.conversion.Start()
	pathValue := uint64(0)
	selectedEnd := start
	selectedValue := uint64(0)
	for end := start; end < min(len(units), start+cjkMaximumWordCharacters); end++ {
		for _, character := range []byte(units[end].term) {
			var transitionValue uint64
			state, transitionValue = dictionary.conversion.AcceptWithVal(state, character)
			pathValue += transitionValue
			if !dictionary.conversion.CanMatch(state) {
				return cjkConversionOutput(dictionary, selectedEnd, selectedValue, start)
			}
		}
		if matched, value := dictionary.conversion.IsMatchWithVal(state); matched {
			selectedEnd = end + 1
			selectedValue = pathValue + value
		}
	}

	return cjkConversionOutput(dictionary, selectedEnd, selectedValue, start)
}

func cjkConversionOutput(
	dictionary *cjkChineseDictionary,
	end int,
	value uint64,
	start int,
) (int, string, bool) {
	if end == start {
		return start, "", false
	}
	offset := int(value >> 16)
	length := int(value & 0xFFFF)
	if offset < 0 || length <= 0 || offset+length > len(dictionary.conversionOutput) {
		return start, "", false
	}

	return end, string(dictionary.conversionOutput[offset : offset+length]), true
}

func dictionaryCJKTokens(
	units []cjkTextUnit,
	basePosition int,
	dictionary *vellum.FST,
) analysis.TokenStream {
	segments := dictionaryCJKSegments(units, dictionary)
	output := make(analysis.TokenStream, 0, len(units)*2+len(segments))
	for index, unit := range units {
		position := basePosition + index
		output = append(output, &analysis.Token{
			Term:     []byte(unit.term),
			Start:    unit.start,
			End:      unit.end,
			Position: position,
			Type:     analysis.Single,
		})
		if index+1 < len(units) {
			output = append(output, &analysis.Token{
				Term:     []byte(unit.term + units[index+1].term),
				Start:    unit.start,
				End:      units[index+1].end,
				Position: position,
				Type:     analysis.Double,
			})
		}
		if segmentEnd := segments[index]; segmentEnd-index > 2 {
			term := strings.Builder{}
			for segmentIndex := index; segmentIndex < segmentEnd; segmentIndex++ {
				term.WriteString(units[segmentIndex].term)
			}
			output = append(output, &analysis.Token{
				Term:     []byte(term.String()),
				Start:    unit.start,
				End:      units[segmentEnd-1].end,
				Position: position,
				Type:     analysis.Shingle,
			})
		}
	}

	return output
}

func dictionaryCJKSegments(units []cjkTextUnit, dictionary *vellum.FST) []int {
	segments := make([]int, len(units))
	if dictionary == nil {
		return segments
	}
	scores := make([]int, len(units)+1)
	for start := len(units) - 1; start >= 0; start-- {
		segments[start], scores[start] = bestCJKSegment(units, dictionary, scores, start)
	}

	return segments
}

func bestCJKSegment(
	units []cjkTextUnit,
	dictionary *vellum.FST,
	scores []int,
	start int,
) (int, int) {
	selectedEnd := start + 1
	selectedScore := scores[start+1] + 1
	state := dictionary.Start()
	for end := start; end < min(len(units), start+cjkMaximumWordCharacters); end++ {
		var available bool
		state, available = advanceCJKDictionaryState(dictionary, state, units[end].term)
		if !available {
			break
		}
		wordLength := end - start + 1
		candidate := scores[end+1] + wordLength*wordLength
		if wordLength >= 2 && dictionary.IsMatch(state) && candidate > selectedScore {
			selectedEnd = end + 1
			selectedScore = candidate
		}
	}

	return selectedEnd, selectedScore
}

func advanceCJKDictionaryState(
	dictionary *vellum.FST,
	state int,
	term string,
) (int, bool) {
	for _, character := range []byte(term) {
		state = dictionary.Accept(state, character)
		if !dictionary.CanMatch(state) {
			return state, false
		}
	}

	return state, true
}

func isCJKAnalyzer(name string) bool {
	switch name {
	case "cjk", cjkChineseTextAnalyzer, cjkJapaneseTextAnalyzer, cjkKoreanTextAnalyzer,
		cjkChineseQueryAnalyzer, cjkJapaneseQueryAnalyzer, cjkKoreanQueryAnalyzer:
		return true
	default:
		return false
	}
}

func cjkQueryAnalyzer(name string) string {
	switch name {
	case cjkChineseTextAnalyzer:
		return cjkChineseQueryAnalyzer
	case cjkJapaneseTextAnalyzer:
		return cjkJapaneseQueryAnalyzer
	case cjkKoreanTextAnalyzer:
		return cjkKoreanQueryAnalyzer
	default:
		return name
	}
}
