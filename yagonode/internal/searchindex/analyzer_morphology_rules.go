package searchindex

import (
	"github.com/blevesearch/snowballstem"
	"github.com/blevesearch/snowballstem/arabic"
	"github.com/blevesearch/snowballstem/danish"
	"github.com/blevesearch/snowballstem/dutch"
	"github.com/blevesearch/snowballstem/english"
	"github.com/blevesearch/snowballstem/finnish"
	"github.com/blevesearch/snowballstem/french"
	"github.com/blevesearch/snowballstem/german"
	"github.com/blevesearch/snowballstem/hungarian"
	"github.com/blevesearch/snowballstem/italian"
	"github.com/blevesearch/snowballstem/norwegian"
	"github.com/blevesearch/snowballstem/portuguese"
	"github.com/blevesearch/snowballstem/romanian"
	"github.com/blevesearch/snowballstem/russian"
	"github.com/blevesearch/snowballstem/spanish"
	"github.com/blevesearch/snowballstem/swedish"
	"github.com/blevesearch/snowballstem/turkish"
)

var analyzerMorphologyRuleSets = map[string][][]*snowballstem.Among{
	"ar": {
		arabic.A_0, arabic.A_1, arabic.A_2, arabic.A_3,
		arabic.A_4, arabic.A_5, arabic.A_6, arabic.A_7,
		arabic.A_8, arabic.A_9, arabic.A_10, arabic.A_11,
		arabic.A_12, arabic.A_13, arabic.A_14, arabic.A_15,
		arabic.A_16, arabic.A_17, arabic.A_18, arabic.A_19,
		arabic.A_20, arabic.A_21,
	},
	"da": {danish.A_0, danish.A_1, danish.A_2},
	"de": {
		german.A_0, german.A_1, german.A_2, german.A_3, german.A_4,
	},
	"en": {
		english.A_0, english.A_1, english.A_2, english.A_3,
		english.A_4, english.A_5, english.A_6, english.A_7,
		english.A_8, english.A_9, english.A_10,
	},
	"es": {
		spanish.A_0, spanish.A_1, spanish.A_2, spanish.A_3,
		spanish.A_4, spanish.A_5, spanish.A_6, spanish.A_7,
		spanish.A_8, spanish.A_9,
	},
	"fi": {
		finnish.A_0, finnish.A_1, finnish.A_2, finnish.A_3,
		finnish.A_4, finnish.A_5, finnish.A_6, finnish.A_7,
		finnish.A_8, finnish.A_9,
	},
	"fr": {
		french.A_0, french.A_1, french.A_2, french.A_3,
		french.A_4, french.A_5, french.A_6, french.A_7,
		french.A_8,
	},
	"hu": {
		hungarian.A_0, hungarian.A_1, hungarian.A_2, hungarian.A_3,
		hungarian.A_4, hungarian.A_5, hungarian.A_6, hungarian.A_7,
		hungarian.A_8, hungarian.A_9, hungarian.A_10, hungarian.A_11,
	},
	"it": {
		italian.A_0, italian.A_1, italian.A_2, italian.A_3,
		italian.A_4, italian.A_5, italian.A_6, italian.A_7,
	},
	"nl": {
		dutch.A_0, dutch.A_1, dutch.A_2, dutch.A_3, dutch.A_4, dutch.A_5,
	},
	"no": {
		norwegian.A_0, norwegian.A_1, norwegian.A_2,
	},
	"pt": {
		portuguese.A_0, portuguese.A_1, portuguese.A_2, portuguese.A_3,
		portuguese.A_4, portuguese.A_5, portuguese.A_6, portuguese.A_7,
		portuguese.A_8,
	},
	"ro": {
		romanian.A_0, romanian.A_1, romanian.A_2,
		romanian.A_3, romanian.A_4, romanian.A_5,
	},
	"ru": {
		russian.A_0, russian.A_1, russian.A_2, russian.A_3,
		russian.A_4, russian.A_5, russian.A_6, russian.A_7,
	},
	"sv": {swedish.A_0, swedish.A_1, swedish.A_2},
	"tr": {
		turkish.A_0, turkish.A_1, turkish.A_2, turkish.A_3,
		turkish.A_4, turkish.A_5, turkish.A_6, turkish.A_7,
		turkish.A_8, turkish.A_9, turkish.A_10, turkish.A_11,
		turkish.A_12, turkish.A_13, turkish.A_14, turkish.A_15,
		turkish.A_16, turkish.A_17, turkish.A_18, turkish.A_19,
		turkish.A_20, turkish.A_21, turkish.A_22, turkish.A_23,
	},
}

func analyzerMorphologyRules(analyzer string) [][]*snowballstem.Among {
	return analyzerMorphologyRuleSets[analyzer]
}
