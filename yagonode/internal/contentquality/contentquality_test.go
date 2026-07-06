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
		"lorem-ipsum": naturalText(10) + " Lorem ipsum dolor sit amet.",
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
