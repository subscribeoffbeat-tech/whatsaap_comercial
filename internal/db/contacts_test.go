package db

import (
	"testing"
)

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		// Valid inputs
		{"+919876543210", "+919876543210", false},
		{"919876543210", "+919876543210", false},
		{"+1-800-555-0199", "+18005550199", false},
		{" +44 20 7946 0958 ", "+442079460958", false},
		{"+91 98765 43210", "+919876543210", false},
		{"(+1) 212.555.0100", "+12125550100", false},
		{"+6598765432", "+6598765432", false}, // 8-digit Singapore number
		// Already normalised
		{"+12025550199", "+12025550199", false},

		// Invalid inputs
		{"", "", true},
		{"abc", "", true},
		{"12345", "", true},        // too short
		{"0044207946095812345678", "", true}, // too long
		{"+", "", true},
		{"++1234567890", "", true},
	}

	for _, tc := range tests {
		got, err := NormalizePhone(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("NormalizePhone(%q): expected error, got %q", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizePhone(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizePhone(%q): got %q, want %q", tc.input, got, tc.want)
		}
	}
}
