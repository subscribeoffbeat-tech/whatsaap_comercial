package templates

import "strings"

var indianCities = []string{
	"Ahmedabad", "Bengaluru", "Bhopal", "Chennai", "Coimbatore",
	"Delhi", "Faridabad", "Ghaziabad", "Gurugram", "Hyderabad",
	"Indore", "Jaipur", "Kanpur", "Kochi", "Kolkata",
	"Lucknow", "Ludhiana", "Mumbai", "Nagpur", "Nashik",
	"Patna", "Pune", "Rajkot", "Surat", "Thiruvananthapuram",
	"Vadodara", "Varanasi", "Visakhapatnam",
}

const ncChevronLeftSVG = `<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M10 3L5 8l5 5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`

// jsStr escapes a Go string for safe embedding as a JS string literal (single-quoted).
func jsStr(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
