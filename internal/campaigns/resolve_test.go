package campaigns

import (
	"testing"

	"whatsapptool/internal/db"
)

func TestResolveVars(t *testing.T) {
	email := "rahul@brandco.in"
	c := db.Contact{
		Name:     "Rahul Mehta",
		WAPhone:  "+919811111111",
		Email:    &email,
		Industry: "FMCG",
		CustomFields: map[string]any{
			"brand": "BrandCo",
			"city":  "Mumbai",
		},
	}
	varMap := map[string]string{
		"1": "first_name",          // derived from full name
		"2": "name",                // full name
		"3": "industry",            // standard column
		"4": "custom_fields.brand", // agency custom field
		"5": "custom_fields.gone",  // missing → fallback
		"6": "email",
	}
	fallbacks := map[string]string{
		"1": "there",
		"2": "Customer",
		"3": "your sector",
		"4": "your brand",
		"5": "N/A",
		"6": "—",
	}

	got := ResolveVars(c, varMap, fallbacks)
	want := []string{"Rahul", "Rahul Mehta", "FMCG", "BrandCo", "N/A", "rahul@brandco.in"}

	if len(got) != len(want) {
		t.Fatalf("ResolveVars len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("var {{%d}} = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestResolveVarsFallbackOnEmptyName(t *testing.T) {
	c := db.Contact{Name: "", CustomFields: map[string]any{}}
	got := ResolveVars(c,
		map[string]string{"1": "first_name"},
		map[string]string{"1": "there"})
	if len(got) != 1 || got[0] != "there" {
		t.Errorf("empty name first_name = %v, want [there]", got)
	}
}
