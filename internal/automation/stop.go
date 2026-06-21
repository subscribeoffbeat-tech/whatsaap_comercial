package automation

import (
	"strings"
)

var stopKeywords = []string{
	"STOP", "UNSUBSCRIBE", "OPT OUT", "OPTOUT", "OPT-OUT", "CANCEL",
}

// IsStopKeyword returns true if the trimmed, uppercased text exactly matches
// any recognised opt-out keyword.
func IsStopKeyword(text string) bool {
	upper := strings.ToUpper(strings.TrimSpace(text))
	for _, kw := range stopKeywords {
		if upper == kw {
			return true
		}
	}
	return false
}

// StopConfirmMessage is sent to the contact after a successful opt-out.
const StopConfirmMessage = "You have been unsubscribed and will no longer receive " +
	"marketing messages from us. Reply START to opt back in."
