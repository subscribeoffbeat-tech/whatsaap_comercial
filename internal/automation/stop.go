package automation

import (
	"strings"
)

var stopKeywords = []string{
	"STOP", "UNSUBSCRIBE", "OPT OUT", "OPTOUT", "OPT-OUT", "CANCEL",
}

// IsStopKeyword returns true if the trimmed, uppercased text exactly matches
// any built-in opt-out keyword.
func IsStopKeyword(text string) bool {
	return MatchesStopKeyword(text, nil)
}

// MatchesStopKeyword checks the text against the mandatory built-in opt-out
// keywords PLUS any admin-configured extras. The built-ins always opt out and
// cannot be removed via config (compliance guarantee).
//
// Matching tolerates surrounding punctuation/whitespace, so "STOP.", "Stop!",
// and " stop " all opt out — regulators expect these common variants to work —
// while still requiring the message to BE the keyword (not merely contain it),
// so "please stop sending so much" is not treated as an opt-out.
func MatchesStopKeyword(text string, configured []string) bool {
	norm := normalizeKeyword(text)
	if norm == "" {
		return false
	}
	for _, kw := range stopKeywords {
		if norm == normalizeKeyword(kw) {
			return true
		}
	}
	for _, kw := range configured {
		if norm == normalizeKeyword(kw) {
			return true
		}
	}
	return false
}

// normalizeKeyword upcases and strips leading/trailing punctuation and spaces so
// keyword comparison is tolerant of "STOP.", "Stop!", " stop ", etc. Interior
// spacing is collapsed so "OPT  OUT" still matches "OPT OUT".
func normalizeKeyword(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.Trim(s, ".!?,;:'\"()[]{} \t\r\n")
	return strings.Join(strings.Fields(s), " ")
}

// StopConfirmMessage is sent to the contact after a successful opt-out.
const StopConfirmMessage = "You have been unsubscribed and will no longer receive " +
	"marketing messages from us. Reply START to opt back in."
