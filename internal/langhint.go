package internal

import (
	"strings"
	"sync/atomic"
	"unicode"
)

// Hardcover leaves the language of many editions blank, so foreign editions
// (and the works they title) slip past downstream language filters. When
// enabled, guessLanguage labels a blank-language edition as foreign if its
// title gives clear evidence. Anything ambiguous stays blank, so this only
// ever acts as a hint and never hides a title that might be English.

var inferLanguage atomic.Bool

// SetInferLanguage toggles labeling of blank-language editions.
func SetInferLanguage(on bool) { inferLanguage.Store(on) }

// languageOrGuess returns code unless it's blank and inference is enabled.
func languageOrGuess(code, title string) string {
	if code != "" || !inferLanguage.Load() {
		return code
	}
	return guessLanguage(title)
}

// Function words that are not English words, so they are good evidence. Words
// that collide with English or with several languages ("die", "in", "an",
// "la", "de", "a") are deliberately omitted or given little weight.
var _langWords = map[string]map[string]float64{
	"ger": words(2, "der", "das", "und", "ein", "eine", "einer", "von", "mit", "dem", "den", "des", "im", "nicht", "ist"),
	"fre": words(2, "les", "des", "une", "du", "et", "au", "aux", "dans", "pour", "sur", "est"),
	"spa": words(2, "el", "los", "las", "del", "una", "por", "con", "para", "que"),
	"ita": words(2, "il", "gli", "della", "delle", "degli", "uno", "per", "con"),
	"por": words(2, "os", "uma", "dos", "das", "não"),
	"dut": words(2, "het", "een", "van", "voor", "met"),
	"swe": words(2, "och", "det", "den", "ett", "att"),
	"dan": words(2, "og", "er", "ikke"),
}

// Letters that point at a language without being English.
var _langLetters = map[rune]struct {
	codes  []string
	weight float64
}{
	'ß': {[]string{"ger"}, 3}, 'ñ': {[]string{"spa"}, 2}, '¿': {[]string{"spa"}, 2}, '¡': {[]string{"spa"}, 2},
	'œ': {[]string{"fre"}, 1.5}, 'ê': {[]string{"fre"}, 1}, 'è': {[]string{"fre"}, 1}, 'ç': {[]string{"fre"}, 1},
	'ã': {[]string{"por"}, 1.5}, 'õ': {[]string{"por"}, 1.5},
	'å': {[]string{"swe"}, 1.5}, 'ø': {[]string{"dan"}, 2}, 'æ': {[]string{"dan"}, 1.5},
	'ä': {[]string{"ger", "swe"}, 1}, 'ö': {[]string{"ger", "swe"}, 1}, 'ü': {[]string{"ger"}, 1},
}

// English words are the counterweight. "a", "an" and "in" are weak because
// other languages use them too.
var _englishWords = map[string]float64{
	"the": 3, "of": 2, "and": 2, "to": 2, "for": 1.5, "with": 1.5, "from": 1.5,
	"is": 1.5, "my": 1.5, "your": 1.5, "his": 1.5, "her": 1.5, "you": 1.5,
	"at": 1, "a": 0.5, "an": 0.5, "in": 0.5,
}

func words(weight float64, ws ...string) map[string]float64 {
	m := make(map[string]float64, len(ws))
	for _, w := range ws {
		m[w] = weight
	}
	return m
}

// Non-Latin scripts and the language code we label them with. Only the fact
// that it isn't English matters downstream.
var _scripts = []struct {
	table *unicode.RangeTable
	code  string
}{
	{unicode.Cyrillic, "rus"}, {unicode.Greek, "gre"}, {unicode.Arabic, "ara"},
	{unicode.Hebrew, "heb"}, {unicode.Han, "chi"}, {unicode.Hiragana, "jpn"},
	{unicode.Katakana, "jpn"}, {unicode.Hangul, "kor"}, {unicode.Thai, "tha"},
	{unicode.Bengali, "ben"}, {unicode.Devanagari, "hin"},
}

// guessLanguage returns a language code for a title that is clearly not
// English, or "" if the title is English or can't be told.
func guessLanguage(title string) string {
	letters, latin := 0, 0
	other := map[string]int{}
	for _, r := range title {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Latin, r) {
			latin++
			continue
		}
		for _, s := range _scripts {
			if unicode.Is(s.table, r) {
				other[s.code]++
				break
			}
		}
	}
	if letters == 0 {
		return ""
	}

	english := 0.0
	foreign := map[string]float64{}
	for _, w := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	}) {
		english += _englishWords[w]
		for code, ws := range _langWords {
			foreign[code] += ws[w]
		}
	}
	for _, r := range strings.ToLower(title) {
		if l, ok := _langLetters[r]; ok {
			for _, code := range l.codes {
				foreign[code] += l.weight
			}
		}
	}

	// Mostly non-Latin: label by the dominant script, unless the title also
	// carries a clear English hint (e.g. "The Shining: <Arabic subtitle>").
	if latin*2 < letters {
		if english >= 2 {
			return ""
		}
		best, n := "", 0
		for code, c := range other {
			if c > n {
				best, n = code, c
			}
		}
		return best
	}

	best, score := "", 0.0
	for code, s := range foreign {
		if s > score || (s == score && code < best) {
			best, score = code, s
		}
	}

	// A lone function word isn't enough ("El Deafo"). Require clear foreign
	// evidence with no English hint, or evidence that outweighs the English hints.
	if score >= 3 && (english == 0 || score-english >= 3) {
		return best
	}
	return ""
}
