package pageparse

import (
	"strings"
	"testing"
)

const boilerplateFixture = `<!doctype html><html><head><title>Статья</title></head><body>
<nav><ul>
<li><a href="/">Главная</a></li><li><a href="/catalog">Каталог товаров</a></li>
<li><a href="/cart">Корзина покупателя</a></li><li><a href="/login">Личный кабинет</a></li>
</ul></nav>
<aside>Подпишитесь на нашу рассылку и получите скидку десять процентов на первый заказ.</aside>
<main><article>
<h1>Как устроен адаптивный краулер</h1>
<p>Адаптивный краулер измеряет отклик сервера и расширяет паузу между запросами,
когда сервер сигнализирует о перегрузке кодами четыреста двадцать девять или пятьсот три.
Такой подход бережёт чужие мощности и репутацию собственного узла.</p>
<p>Восстановление после затишья происходит постепенно: каждый успешный запрос
сокращает штраф вдвое, пока пауза не вернётся к базовому ритму вежливости.</p>
</article></main>
<footer>
<p>Все права защищены. Политика конфиденциальности. Пользовательское соглашение.
Карта сайта. Контакты компании. Реквизиты организации.</p>
</footer>
</body></html>`

// TestParseHTMLExtractsMainContentWithoutBoilerplate is the CRAWL-13
// acceptance: article text is indexed, navigation/aside/footer chrome is not.
func TestParseHTMLExtractsMainContentWithoutBoilerplate(t *testing.T) {
	page := ParseHTML("https://example.org/article", "text/html", []byte(boilerplateFixture))
	for _, want := range []string{
		"адаптивный краулер",
		"сокращает штраф вдвое",
	} {
		if !strings.Contains(strings.ToLower(page.Text), want) {
			t.Fatalf("main content lost %q in: %.300s", want, page.Text)
		}
	}
	for _, chrome := range []string{
		"Корзина покупателя",
		"Личный кабинет",
		"рассылку",
		"Политика конфиденциальности",
		"Карта сайта",
	} {
		if strings.Contains(page.Text, chrome) {
			t.Fatalf("boilerplate %q leaked into: %.400s", chrome, page.Text)
		}
	}
}
