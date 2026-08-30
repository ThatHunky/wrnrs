package play_test

import (
	"os"
	"sort"
	"strings"
	"testing"
	"unicode"

	"wrnrs/internal/catalog"
)

func loadPlayCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	file, err := os.Open("../../content/play.v1.json")
	if err != nil {
		t.Fatalf("open play catalog: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	c, err := catalog.Load(file)
	if err != nil {
		t.Fatalf("load play catalog: %v", err)
	}
	return c
}

func TestPlayCatalogValidatesForBothLanguages(t *testing.T) {
	if err := loadPlayCatalog(t).Validate([]string{"uk", "en"}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestPlayCatalogHasExactlyEightyPaddedAscendingIDs(t *testing.T) {
	c := loadPlayCatalog(t)
	if len(c.Items) != 80 {
		t.Fatalf("catalog has %d items, want exactly 80", len(c.Items))
	}
	for i, item := range c.Items {
		if len(item.ID) != 4 || !strings.HasPrefix(item.ID, "p") {
			t.Fatalf("item %d id = %q, want the zero-padded form pNNN", i, item.ID)
		}
		if i > 0 && c.Items[i-1].ID >= item.ID {
			t.Fatalf("ids are not strictly ascending at %d: %q then %q", i, c.Items[i-1].ID, item.ID)
		}
	}
}

func TestPlayCatalogFacetDistributionIsExact(t *testing.T) {
	kinds := map[string]int{"truth": 0, "dare": 0}
	intensities := map[string]int{"gentle": 0, "medium": 0, "bold": 0}
	perKind := map[string]map[string]int{
		"truth": {"gentle": 0, "medium": 0, "bold": 0},
		"dare":  {"gentle": 0, "medium": 0, "bold": 0},
	}

	for _, item := range loadPlayCatalog(t).Items {
		ks := item.Facets["kind"]
		if len(ks) != 1 {
			t.Fatalf("item %s has %d kind values, want exactly one", item.ID, len(ks))
		}
		if _, ok := kinds[ks[0]]; !ok {
			t.Fatalf("item %s has unknown kind %q", item.ID, ks[0])
		}
		is := item.Facets["intensity"]
		if len(is) != 1 {
			t.Fatalf("item %s has %d intensity values, want exactly one", item.ID, len(is))
		}
		if _, ok := intensities[is[0]]; !ok {
			t.Fatalf("item %s has unknown intensity %q", item.ID, is[0])
		}
		kinds[ks[0]]++
		intensities[is[0]]++
		perKind[ks[0]][is[0]]++
	}

	if kinds["truth"] != 40 || kinds["dare"] != 40 {
		t.Fatalf("kind split = truth %d / dare %d, want 40/40", kinds["truth"], kinds["dare"])
	}
	if intensities["gentle"] != 30 || intensities["medium"] != 30 || intensities["bold"] != 20 {
		t.Fatalf("intensity split = %v, want gentle 30 / medium 30 / bold 20", intensities)
	}
	for kind, want := range map[string]map[string]int{
		"truth": {"gentle": 15, "medium": 15, "bold": 10},
		"dare":  {"gentle": 15, "medium": 15, "bold": 10},
	} {
		for intensity, n := range want {
			if perKind[kind][intensity] != n {
				t.Fatalf("%s/%s = %d, want %d", kind, intensity, perKind[kind][intensity], n)
			}
		}
	}
}

func TestPlayCatalogTextLengthsAreWithinBounds(t *testing.T) {
	for _, item := range loadPlayCatalog(t).Items {
		for _, lang := range []string{"uk", "en"} {
			title := []rune(item.Text[lang].Title)
			body := []rune(item.Text[lang].Body)
			if len(title) == 0 || len(title) > 40 {
				t.Fatalf("item %s %s title is %d runes, want 1-40", item.ID, lang, len(title))
			}
			if len(body) < 30 || len(body) > 160 {
				t.Fatalf("item %s %s body is %d runes, want 30-160", item.ID, lang, len(body))
			}
		}
	}
}

func TestPlayCatalogTruthsAskAndDaresInstruct(t *testing.T) {
	for _, item := range loadPlayCatalog(t).Items {
		isTruth := item.Facets["kind"][0] == "truth"
		for _, lang := range []string{"uk", "en"} {
			body := item.Text[lang].Body
			endsWithQuestion := strings.HasSuffix(strings.TrimSpace(body), "?")
			if isTruth && !endsWithQuestion {
				t.Fatalf("truth %s %s body does not end in a question mark: %q", item.ID, lang, body)
			}
			if !isTruth && endsWithQuestion {
				t.Fatalf("dare %s %s body reads as a question: %q", item.ID, lang, body)
			}
		}
	}
}

// --- gendered-Ukrainian guard -------------------------------------------------
//
// Every card is dealt to whichever partner's turn it is, and the caption prints
// that partner's name above it. A Ukrainian form that agrees with a *person* —
// a past-tense verb, a predicative adjective or participle, «сам»/«сама» —
// therefore contradicts the name it was just printed under half the time.
//
// The check below is deliberately built for precision, not recall: a naive
// "flag every word ending in -а/-ий" sweep flags «Зупинена мить» and
// «Непромовлена думка», which agree with a feminine NOUN and are perfectly
// correct. So each layer only fires where the agreement target can only be a
// person. If you add card 81 and this test flags a form that agrees with a
// noun, the pattern is wrong — narrow it here rather than rewording the card.

// personTokens are forms that can only ever agree with a person, in any
// position. «сам»/«сама» and their case forms qualify; so do «міг»/«могла».
// The «ви» pronouns are here for a different reason: every card in this
// catalog addresses the partner as «ти», so any «ви» form is wrong register
// even where it is grammatical.
var personTokens = map[string]bool{
	"сам": true, "сама": true, "самому": true, "самій": true, "самим": true,
	"міг": true, "могла": true,
	"вам": true, "вами": true, "ваш": true, "ваша": true,
	"ваше": true, "ваші": true, "вашого": true, "вашій": true, "вашим": true,
}

// predicativeTokens are adjectives and participles that agree with a person
// only when they stand predicatively. Attributively they agree with their head
// noun and are fine: «Вдячний поцілунок» is a grateful kiss, «ти вдячний» is a
// man. The two are told apart by what follows the token — see
// attributiveFollowers.
var predicativeTokens = map[string]bool{
	"вдячний": true, "вдячна": true,
	"бажаний": true, "бажана": true,
	"поміченим": true, "поміченою": true,
	"притягнутий": true, "притягнута": true,
	"притиснутий": true, "притиснута": true,
}

// attributiveFollowers are the words that can NOT be the head noun of a
// preceding adjective — prepositions, particles, conjunctions and adverbs. A
// predicativeToken followed by one of these (or by nothing at all) has no head
// noun, so it is agreeing with a person.
var attributiveFollowers = map[string]bool{
	"до": true, "на": true, "в": true, "у": true, "з": true, "зі": true, "із": true,
	"за": true, "під": true, "над": true, "при": true, "про": true, "від": true,
	"для": true, "без": true, "не": true, "ні": true, "ще": true, "вже": true,
	"тільки": true, "саме": true, "так": true, "близько": true, "поруч": true,
	"сьогодні": true, "зараз": true, "тут": true, "там": true, "завжди": true,
	"ніколи": true, "і": true, "й": true, "та": true, "а": true, "але": true,
	"бо": true, "що": true, "коли": true, "поки": true, "як": true,
}

// pastParticles are the negations and adverbs that introduce a past-tense verb
// with an implied personal subject, as in «Ще не цілував».
var pastParticles = map[string]bool{"не": true, "ще": true, "ніколи": true, "вже": true}

// ukrainianWords splits text into lowercase word tokens. Apostrophes stay
// inside a word («зап'ястя»); everything else separates.
func ukrainianWords(text string) []string {
	var words []string
	var current []rune
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || r == '\'' || r == 'ʼ' || r == '’' {
			current = append(current, r)
			continue
		}
		if len(current) > 0 {
			words = append(words, string(current))
			current = nil
		}
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}

// looksPastTense reports whether word is long enough to be a gendered past
// tense form (masculine -в, feminine -ла) rather than a short function word
// that merely ends in those letters. minStem is the number of runes required
// before the ending.
func looksPastTense(word string, minStem int) bool {
	runes := []rune(word)
	if strings.HasSuffix(word, "в") {
		return len(runes) >= minStem+1
	}
	if strings.HasSuffix(word, "ла") {
		return len(runes) >= minStem+2
	}
	return false
}

// genderedForms reports every person-agreeing form in text, as
// "rule:token" strings.
func genderedForms(text string) []string {
	words := ukrainianWords(text)
	seen := map[string]bool{}
	var found []string
	add := func(rule, token string) {
		key := rule + ":" + token
		if seen[token] {
			return
		}
		seen[token] = true
		found = append(found, key)
	}
	for i, word := range words {
		if personTokens[word] {
			add("person-only form", word)
		}
		if predicativeTokens[word] {
			if i+1 >= len(words) || attributiveFollowers[words[i+1]] {
				add("predicative adjective", word)
			}
		}
		if word == "я" || word == "ти" {
			// «я міг би», «ти ще ніколи не промовляв»: the subject is right
			// there, so a past tense within a few words of it agrees with it.
			for j := i + 1; j < len(words) && j <= i+4; j++ {
				if looksPastTense(words[j], 2) {
					add("past tense after «"+word+"»", words[j])
					break
				}
			}
		}
		if pastParticles[word] && i+1 < len(words) && looksPastTense(words[i+1], 3) {
			add("past tense after «"+word+"»", words[i+1])
		}
	}
	return found
}

func TestPlayCatalogUkrainianCardsReadForEitherPartner(t *testing.T) {
	type flag struct {
		id, field, form string
	}
	var flagged []flag
	for _, item := range loadPlayCatalog(t).Items {
		for field, text := range map[string]string{
			"title": item.Text["uk"].Title,
			"body":  item.Text["uk"].Body,
		} {
			for _, form := range genderedForms(text) {
				flagged = append(flagged, flag{item.ID, field, form})
			}
		}
	}
	sort.Slice(flagged, func(i, j int) bool {
		if flagged[i].id != flagged[j].id {
			return flagged[i].id < flagged[j].id
		}
		if flagged[i].field != flagged[j].field {
			return flagged[i].field < flagged[j].field
		}
		return flagged[i].form < flagged[j].form
	})
	for _, f := range flagged {
		t.Errorf("card %s uk %s agrees with one gender: %s — the caption prints a partner's name above this card, so it must read for either partner", f.id, f.field, f.form)
	}
	if len(flagged) > 0 {
		t.Fatalf("%d gendered form(s) in the Ukrainian catalog; rewrite them impersonally (present tense, noun phrases, «важко», «не наважуєшся») rather than with slash forms like «вдячний(-а)»", len(flagged))
	}
}
