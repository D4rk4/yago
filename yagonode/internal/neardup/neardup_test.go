package neardup

import (
	"fmt"
	"testing"
)

const forumText = "Обсуждение и настройка маршрутизатора платформа гипервизор версия " +
	"подключение интерфейс конфигурация туннель политика маршрут перезагрузка " +
	"обновление прошивка консоль команда вывод журнал ошибка отладка replication " +
	"cluster failover балансировка мониторинг производительность лицензия образ"

func TestObserveDetectsNearDuplicateOfDifferentKey(t *testing.T) {
	window := NewWindow(8)
	if _, dup := window.Observe("https://a.example/one", forumText); dup {
		t.Fatal("first observation flagged duplicate")
	}
	original, dup := window.Observe(
		"https://a.example/one?session=2",
		forumText+" футер",
	)
	if !dup || original != "https://a.example/one" {
		t.Fatalf("near-duplicate missed: %q %v", original, dup)
	}
}

func TestObserveRefreshesSameKeyWithoutDuplicating(t *testing.T) {
	window := NewWindow(8)
	window.Observe("https://a.example/one", forumText)
	if _, dup := window.Observe("https://a.example/one", forumText+" обновлено"); dup {
		t.Fatal("refetch of the same key flagged duplicate")
	}
}

func TestObserveIgnoresShortAndDistinctTexts(t *testing.T) {
	window := NewWindow(8)
	window.Observe("https://a.example/one", forumText)
	if _, dup := window.Observe("https://a.example/two", "слишком мало слов"); dup {
		t.Fatal("short text flagged duplicate")
	}
	distinct := "Золотая осень листва парки прогулки фотографии стихи ноябрь " +
		"деревья погода настроение природа"
	if _, dup := window.Observe("https://b.example/autumn", distinct); dup {
		t.Fatal("distinct text flagged duplicate")
	}
}

func TestWindowEvictsOldestBeyondCapacity(t *testing.T) {
	window := NewWindow(2)
	texts := []string{
		"золотая осень листва парки прогулки фотографии стихи ноябрь погода деревья",
		"рецепт борща свекла капуста мясо бульон сметана зелень кастрюля плита",
		"футбольный матч гол вратарь стадион болельщики судья пенальти тайм счет",
	}
	for i, text := range texts {
		if _, dup := window.Observe(fmt.Sprintf("k%d", i), text); dup {
			t.Fatalf("distinct doc %d flagged duplicate", i)
		}
	}
	if got := len(window.entries); got != 2 {
		t.Fatalf("window grew past capacity: %d", got)
	}
}

func TestNewWindowDefaultsCapacity(t *testing.T) {
	if got := NewWindow(0).capacity; got != DefaultWindowCapacity {
		t.Fatalf("default capacity = %d", got)
	}
}
