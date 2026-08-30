package positions

// This file is a white-box (package positions, not positions_test) test
// file so it can install a test i18n.Bundle into the unexported facetBundle
// package var directly, the same way NewHandler does at construction. Every
// test that touches facetBundle restores it afterwards (t.Cleanup) so it
// never leaks its bundle into an unrelated test running later in the same
// binary.

import (
	"strings"
	"testing"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
)

// withFacetBundle installs bundle as facetBundle for the duration of one
// test and restores whatever was there before once the test finishes.
func withFacetBundle(t *testing.T, bundle *i18n.Bundle) {
	t.Helper()
	previous := facetBundle
	facetBundle = bundle
	t.Cleanup(func() { facetBundle = previous })
}

func newTestBundle() *i18n.Bundle {
	bundle := i18n.NewBundle()
	bundle.Add(i18n.Catalog{Language: "uk", Strings: map[string]string{
		"positions.facet.level":          "Рівень",
		"positions.facet.location":       "Місце",
		"positions.value.level.hard":     "складна",
		"positions.value.location.chair": "стілець",
	}})
	bundle.Add(i18n.Catalog{Language: "en", Strings: map[string]string{
		"positions.facet.level":          "Level",
		"positions.facet.location":       "Location",
		"positions.value.level.hard":     "Hard",
		"positions.value.location.chair": "Chair",
	}})
	return bundle
}

// TestFacetValueLabelFallsBackToRawValueOnMiss pins the required degrade
// path: a facet value with no matching i18n key must render as the raw
// catalog slug, never as the raw i18n key itself (i18n.Bundle.Text returns
// the key unchanged on a miss — see internal/i18n/catalog.go). A future
// taxonomy addition without a matching translation must stay readable
// ("newthing"), not surface an internal key like
// "positions.value.type.newthing".
func TestFacetValueLabelFallsBackToRawValueOnMiss(t *testing.T) {
	withFacetBundle(t, newTestBundle())

	got := facetValueLabel("uk", "type", "newthing")
	if got != "newthing" {
		t.Fatalf("facetValueLabel with a missing key = %q, want the raw value %q", got, "newthing")
	}
	if strings.HasPrefix(got, "positions.") {
		t.Fatalf("facetValueLabel leaked the raw i18n key: %q", got)
	}
}

// TestFacetNameLabelFallsBackToRawFacetOnMiss mirrors the value-label
// fallback test for a facet's own name.
func TestFacetNameLabelFallsBackToRawFacetOnMiss(t *testing.T) {
	withFacetBundle(t, newTestBundle())

	got := facetNameLabel("uk", "newfacet")
	if got != "newfacet" {
		t.Fatalf("facetNameLabel with a missing key = %q, want the raw facet %q", got, "newfacet")
	}
	if strings.HasPrefix(got, "positions.") {
		t.Fatalf("facetNameLabel leaked the raw i18n key: %q", got)
	}
}

// TestFacetLabelsFallBackToRawWithNoBundleInstalled covers the other half
// of the fallback contract: facetBundle itself can be nil (every test in
// this package that never constructs a Handler leaves it that way), and
// that must degrade exactly like a populated bundle with a missing key.
func TestFacetLabelsFallBackToRawWithNoBundleInstalled(t *testing.T) {
	withFacetBundle(t, nil)

	if got := facetValueLabel("uk", "level", "hard"); got != "hard" {
		t.Fatalf("facetValueLabel with no bundle = %q, want %q", got, "hard")
	}
	if got := facetNameLabel("uk", "level"); got != "level" {
		t.Fatalf("facetNameLabel with no bundle = %q, want %q", got, "level")
	}
}

// TestFacetSummaryLocalizesNamesAndValues pins the fix for the reported
// bug: a Ukrainian caption's facet line must read in Ukrainian, not as raw
// English catalog slugs ("hard · chair").
func TestFacetSummaryLocalizesNamesAndValues(t *testing.T) {
	withFacetBundle(t, newTestBundle())

	item := catalog.Item{
		Facets: map[string][]string{"level": {"hard"}, "location": {"chair"}},
	}

	uk := facetSummary("uk", item)
	if strings.Contains(uk, "hard") || strings.Contains(uk, "chair") {
		t.Fatalf("uk facetSummary %q still contains raw English slugs", uk)
	}
	if !strings.Contains(uk, "складна") || !strings.Contains(uk, "стілець") {
		t.Fatalf("uk facetSummary %q is missing the localized values", uk)
	}
	if !strings.Contains(uk, "Рівень") || !strings.Contains(uk, "Місце") {
		t.Fatalf("uk facetSummary %q is missing the localized facet names", uk)
	}

	en := facetSummary("en", item)
	if !strings.Contains(en, "Hard") || !strings.Contains(en, "Chair") {
		t.Fatalf("en facetSummary %q is missing the localized values", en)
	}
}

// TestFiltersKeyboardLocalizesFacetNamesAndValues pins the fix for the
// filter-buttons half of the reported bug.
func TestFiltersKeyboardLocalizesFacetNamesAndValues(t *testing.T) {
	withFacetBundle(t, newTestBundle())

	facets := []FacetOption{{Facet: "level", Values: []string{"hard"}}}
	markup := FiltersKeyboard("uk", catalog.Filter{}, facets)

	var sawHeader, sawValue bool
	var callbackForHard string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			if button.Text == "Рівень" {
				sawHeader = true
			}
			if strings.Contains(button.Text, "складна") {
				sawValue = true
				callbackForHard = button.CallbackData
			}
		}
	}
	if !sawHeader {
		t.Fatalf("filters keyboard %+v is missing the localized facet name header", markup.InlineKeyboard)
	}
	if !sawValue {
		t.Fatalf("filters keyboard %+v is missing the localized facet value", markup.InlineKeyboard)
	}
	if callbackForHard != "pos:filter:level:hard" {
		t.Fatalf("localized button callback data changed: got %q, want the raw slug callback pos:filter:level:hard", callbackForHard)
	}
}
