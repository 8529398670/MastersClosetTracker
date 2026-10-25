package printer

import (
	"strings"
	"testing"
)

// Every advertised language must have a translation entry, and vice-versa, so
// the admin dropdown never offers a language that renders as English.
func TestLanguagesMatchTranslations(t *testing.T) {
	for _, l := range Languages() {
		if _, ok := translations[l.Code]; !ok {
			t.Errorf("language %q (%s) has no translation entry", l.Code, l.Name)
		}
	}
	if len(Languages()) != len(translations) {
		t.Errorf("Languages()=%d but translations=%d — keep them in sync", len(Languages()), len(translations))
	}
}

func TestStringsForFallsBackToEnglish(t *testing.T) {
	if got := stringsFor("zz"); got.PerPerson != translations["en"].PerPerson {
		t.Errorf("unknown code should fall back to English, got %q", got.PerPerson)
	}
}

func TestFrenchAndSpanishStrings(t *testing.T) {
	if !strings.Contains(stringsFor("fr").FamilySize, "Famille") {
		t.Errorf("fr FamilySize missing 'Famille': %q", stringsFor("fr").FamilySize)
	}
	if !strings.Contains(stringsFor("es").PerPerson, "Persona") {
		t.Errorf("es PerPerson missing 'Persona': %q", stringsFor("es").PerPerson)
	}
}

func TestJobLangResolution(t *testing.T) {
	if (Job{Language: "de"}).lang() != "de" {
		t.Error("explicit Language should win")
	}
	if (Job{Spanish: true}).lang() != "es" {
		t.Error("legacy Spanish bool should map to es")
	}
	if (Job{}).lang() != "en" {
		t.Error("default should be en")
	}
}
