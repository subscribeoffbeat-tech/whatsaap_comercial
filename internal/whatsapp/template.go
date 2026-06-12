package whatsapp

import "regexp"

var varRe = regexp.MustCompile(`\{\{(\d+)\}\}`)

// ExtractVariables returns the unique {{N}} placeholder indices found in body,
// in order of first appearance.
func ExtractVariables(body string) []string {
	seen := map[string]bool{}
	var vars []string
	for _, m := range varRe.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			vars = append(vars, m[1])
		}
	}
	return vars
}

// ValidateVariables returns the variable indices that are present in body but
// have no entry in the fallbacks map. A nil return means all variables are covered.
func ValidateVariables(body string, fallbacks map[string]string) []string {
	var missing []string
	for _, v := range ExtractVariables(body) {
		if _, ok := fallbacks[v]; !ok {
			missing = append(missing, v)
		}
	}
	return missing
}
