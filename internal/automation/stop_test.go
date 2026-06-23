package automation_test

import (
	"testing"

	"whatsapptool/internal/automation"
)

func TestIsStopKeyword(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		// exact matches (uppercase)
		{"STOP", true},
		{"UNSUBSCRIBE", true},
		{"OPT OUT", true},
		{"OPTOUT", true},
		{"OPT-OUT", true},
		{"CANCEL", true},

		// case-insensitive
		{"stop", true},
		{"Stop", true},
		{"unsubscribe", true},
		{"opt out", true},
		{"Opt-Out", true},

		// leading/trailing whitespace stripped
		{"  STOP  ", true},
		{"\tcancel\n", true},

		// trailing/leading punctuation tolerated (regulators expect these)
		{"STOP.", true},
		{"Stop!", true},
		{"stop?", true},
		{"unsubscribe.", true},
		{"(STOP)", true},
		{"opt out.", true},

		// NOT a match — partial or different
		{"hello", false},
		{"stop please", false},   // not exact
		{"please stop", false},
		{"re: stop", false},
		{"stop sending so much", false}, // contains but isn't the keyword
		{"", false},
		{"START", false},
	}

	for _, c := range cases {
		got := automation.IsStopKeyword(c.input)
		if got != c.want {
			t.Errorf("IsStopKeyword(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}
