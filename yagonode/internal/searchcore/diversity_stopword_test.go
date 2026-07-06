package searchcore

import "testing"

// TestNearDuplicatesIgnoreSharedFunctionWords is the SEARCH-30 regression: two
// unrelated Russian texts whose query-biased snippets share mostly function
// words must not collapse as near-duplicates — the live bug ate an autumn blog
// post as a "duplicate" of a Cisco forum thread.
func TestNearDuplicatesIgnoreSharedFunctionWords(t *testing.T) {
	forum := Result{
		Title: "Anticisco - Просмотр темы - Cisco ASAv",
		Snippet: "Ну вот я и не понимаю, что я делаю не так и когда еще раз " +
			"платформа под vsphere версия гипервизора не подключается",
		URL: "https://anticisco.ru/forum/viewtopic.php?t=1",
	}
	autumn := Result{
		Title: "Ах, что за прелесть эта осень",
		Snippet: "Ну вот я и не понимаю, что и когда еще раз так " +
			"золотая осень листва парки прогулки фотографии стихи",
		URL: "http://nikulj.blogspot.com/2016/10/blog-post_24.html",
	}
	kept := dropNearDuplicates([]Result{forum, autumn})
	if len(kept) != 2 {
		t.Fatalf("unrelated texts collapsed as near-duplicates: %#v", kept)
	}

	sameThread := forum
	sameThread.URL = "https://anticisco.ru/forum/viewtopic.php?t=1&sid=abc"
	kept = dropNearDuplicates([]Result{forum, sameThread, autumn})
	if len(kept) != 2 {
		t.Fatalf("true duplicate survived or novel text dropped: %d", len(kept))
	}
}

// TestSimhashNeedsEnoughContentTokens proves function words no longer count
// toward the comparability floor.
func TestSimhashNeedsEnoughContentTokens(t *testing.T) {
	if _, comparable := simhash("что такое и как это было когда он она они с ним"); comparable {
		t.Fatal("stopword-only text must not be comparable")
	}
	if _, comparable := simhash(
		"золотая осень листва парки прогулки фотографии стихи ноябрь",
	); !comparable {
		t.Fatal("eight content tokens must be comparable")
	}
}
