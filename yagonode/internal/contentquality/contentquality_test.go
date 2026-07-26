package contentquality

import (
	"fmt"
	"strings"
	"testing"
)

const naturalParagraph = "Адаптивный краулер измеряет отклик сервера и расширяет " +
	"паузу между запросами, когда получает сигнал о перегрузке. Поисковый узел " +
	"хранит собранные документы в шардированном хранилище, а индекс объединяет " +
	"локальные результаты с ответами удалённых пиров через слияние по обратному " +
	"рангу. Канонизация адресов убирает следящие параметры, сортирует остальные " +
	"и склеивает сессионные варианты одной страницы. Вежливый обход бережёт " +
	"чужие мощности, восстанавливая темп постепенно после каждого успешного " +
	"запроса. Качество текста проверяется детерминированными правилами, чтобы " +
	"зеркала, генерированный спам и пустые страницы не попадали в общий индекс " +
	"сети. Оператор может отключить проверку в консоли администратора, если " +
	"его корпус состоит из необычных, но ценных технических материалов."

func naturalText(int) string {
	return naturalParagraph
}

func TestRejectionRuleAcceptsNaturalText(t *testing.T) {
	if rule := RejectionRule(naturalText(12)); rule != "" {
		t.Fatalf("natural text rejected by %q", rule)
	}
}

func TestRejectionRuleNamesEachViolation(t *testing.T) {
	cases := map[string]string{
		"too-few-words":  "мало слов на странице",
		"too-many-words": strings.TrimSpace(strings.Repeat("уникальное слово ", 50001)),
		"word-length": strings.TrimSpace(
			strings.Repeat("бесконечнодлинноесловосклееноебезпробеловздесь ", 60),
		),
		"symbol-ratio": naturalText(10) + strings.TrimSpace(strings.Repeat(" ###… ", 15)),
		"non-alphabetic": naturalText(6) + " " + strings.TrimSpace(
			strings.Repeat("12345 67890 ", 30),
		),
		// Genuine filler: an unbroken run of placeholder words. A page that merely
		// mentions the phrase — typography guides, CSS documentation, the
		// encyclopedia entry — is ordinary content and is covered separately.
		"lorem-ipsum": naturalText(10) + " " + strings.TrimSpace(strings.Repeat(
			"lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod ", 4,
		)),
		"top-ngram": strings.TrimSpace(
			strings.Repeat("это повторяющаяся пара ", 40),
		),
	}
	for want, text := range cases {
		got := RejectionRule(text)
		if got != want {
			t.Fatalf("rule for %s fixture = %q", want, got)
		}
	}
}

func TestRejectionRuleRequiresFunctionWords(t *testing.T) {
	words := make([]string, 0, 60)
	for i := range 60 {
		words = append(words, fmt.Sprintf("термин%02d", i))
	}
	if got := RejectionRule(strings.Join(words, " ")); got != "no-function-words" {
		t.Fatalf("keyword-stuffed text rule = %q", got)
	}
}

func TestRejectionRuleFlagsRepeatedLongGrams(t *testing.T) {
	phrases := []string{
		"купите наш замечательный товар прямо сейчас по лучшей цене",
		"закажите уникальную услугу немедленно со скидкой для новых клиентов",
		"подпишитесь на выгодное предложение сегодня и получите бонусные баллы",
		"скачайте бесплатное приложение быстро без регистрации и ограничений",
		"оформите доставку курьером домой в удобное время без предоплаты",
		"выберите подходящий тариф онлайн за пару минут через сайт",
	}
	var builder strings.Builder
	builder.WriteString(naturalText(1))
	for range 3 {
		for _, phrase := range phrases {
			builder.WriteString(" ")
			builder.WriteString(phrase)
		}
	}
	if got := RejectionRule(builder.String()); got != "repeated-ngram" {
		t.Fatalf("spun repetition rule = %q", got)
	}
}

func TestRejectionRuleSkipsUnsegmentedScripts(t *testing.T) {
	japanese := strings.Repeat("東京は日本の首都であり、世界最大級の都市圏を形成している。", 4)
	if rule := RejectionRule(japanese); rule != "" {
		t.Fatalf("unsegmented-script text rejected by %q", rule)
	}
}

func TestNGramSharesOnShortInputs(t *testing.T) {
	if got := topNGramCharacterShare(nil, 2); got != 0 {
		t.Fatalf("empty top share = %v", got)
	}
	if got := duplicatedNGramCharacterShare(nil, 2); got != 0 {
		t.Fatalf("empty duplicated share = %v", got)
	}
	if got := duplicatedNGramCharacterShare([]string{"одно"}, 2); got != 0 {
		t.Fatalf("short duplicated share = %v", got)
	}
	repeated := []string{"пара", "слов", "пара", "слов", "пара", "слов"}
	if got := duplicatedNGramCharacterShare(repeated, 2); got == 0 {
		t.Fatal("repeated bigram share must be positive")
	}
}

// Both repetition measures are shares of the text, so they can never exceed it.
// Their absence let a defect ship in which overlapping sliding n-grams were
// counted once per occurrence: an ordinary documentation page measured 4.075
// against a 0.15 limit, and the gate discarded encyclopedia articles and
// software manuals as repetition spam.
func TestNGramSharesAreFractions(t *testing.T) {
	texts := map[string]string{
		"natural":       naturalText(200),
		"repeated pair": strings.TrimSpace(strings.Repeat("это повторяющаяся пара ", 60)),
		"single phrase": strings.TrimSpace(
			strings.Repeat("cheap flights to paris book now ", 200),
		),
		"one word":       strings.TrimSpace(strings.Repeat("a ", 200)),
		"two words only": "alpha beta",
	}
	for name, text := range texts {
		words := strings.Fields(text)
		for n := 2; n <= 10; n++ {
			if got := topNGramCharacterShare(words, n); got < 0 || got > 1 {
				t.Fatalf("%s: top share n=%d = %v, want a fraction", name, n, got)
			}
			if got := duplicatedNGramCharacterShare(words, n); got < 0 || got > 1 {
				t.Fatalf("%s: duplicated share n=%d = %v, want a fraction", name, n, got)
			}
		}
	}
}

// A phrase repeated with no gap covers the whole text exactly once, not once
// per overlapping window.
func TestTopNGramShareCountsOverlapOnce(t *testing.T) {
	words := strings.Fields(strings.TrimSpace(strings.Repeat("a ", 200)))

	if got := topNGramCharacterShare(words, 2); got != 1 {
		t.Fatalf("top share = %v, want the whole text covered once", got)
	}
}

// Only repeats are duplication: the first occurrence of a phrase is the page
// saying something. Three occurrences of a two-word phrase leave two repeats,
// so four of the six words are redundant.
func TestDuplicatedShareCountsOnlyRepeats(t *testing.T) {
	words := []string{"пара", "слов", "пара", "слов", "пара", "слов"}

	share := duplicatedNGramCharacterShare(words, 2)
	if want := 16.0 / 24.0; share != want {
		t.Fatalf("duplicated share = %v, want %v", share, want)
	}
}

// Text with no repeated n-gram at all is not repetitive, so the most-frequent
// gram measure must report nothing rather than the size of an arbitrary gram.
func TestTopNGramShareIgnoresUnrepeatedText(t *testing.T) {
	words := strings.Fields(naturalText(120))

	for n := 2; n <= 4; n++ {
		grams, counts, _ := nGramPositions(words, n)
		repeated := false
		for _, gram := range grams {
			if counts[gram] > 1 {
				repeated = true

				break
			}
		}
		if repeated {
			continue
		}
		if got := topNGramCharacterShare(words, n); got != 0 {
			t.Fatalf("unrepeated text scored %v for n=%d", got, n)
		}
	}
}

// Korean separates words with spaces, so its token statistics are meaningful.
// Listing Hangul as unsegmented let every Korean page, keyword spam included,
// skip the gate entirely.
func TestKoreanTextFacesTheGate(t *testing.T) {
	stuffed := make([]string, 0, 60)
	for i := range 60 {
		stuffed = append(stuffed, fmt.Sprintf("검색어%02d", i))
	}

	if got := RejectionRule(strings.Join(stuffed, " ")); got == "" {
		t.Fatal("Korean keyword stuffing bypassed the gate")
	}
}

// A page discussing the placeholder is ordinary content; only a run of filler
// is not.
func TestPlaceholderRuleSpareseMentionPasses(t *testing.T) {
	mention := naturalText(80) + " The phrase lorem ipsum is standard filler copy."

	if got := RejectionRule(mention); got == "lorem-ipsum" {
		t.Fatalf("a page about the placeholder was rejected as filler")
	}
}
