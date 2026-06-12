package whatsapp

import (
	"reflect"
	"testing"
)

func TestExtractVariables(t *testing.T) {
	tests := []struct {
		body string
		want []string
	}{
		{"Hello {{1}}, your order {{2}} is ready.", []string{"1", "2"}},
		{"No variables here.", nil},
		{"Duplicate {{1}} and {{1}} again.", []string{"1"}}, // deduplicated
		{"{{3}} first, then {{1}}, then {{2}}.", []string{"3", "1", "2"}},
		{"", nil},
		{"{{1}}", []string{"1"}},
	}

	for _, tc := range tests {
		got := ExtractVariables(tc.body)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ExtractVariables(%q): got %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestValidateVariables(t *testing.T) {
	tests := []struct {
		body      string
		fallbacks map[string]string
		missing   []string
	}{
		{
			body:      "Hi {{1}}, your code is {{2}}.",
			fallbacks: map[string]string{"1": "there", "2": "123456"},
			missing:   nil,
		},
		{
			body:      "Hi {{1}}, your code is {{2}}.",
			fallbacks: map[string]string{"1": "there"},
			missing:   []string{"2"},
		},
		{
			body:      "Hi {{1}}, your code is {{2}}.",
			fallbacks: map[string]string{},
			missing:   []string{"1", "2"},
		},
		{
			body:      "No variables.",
			fallbacks: map[string]string{},
			missing:   nil,
		},
		{
			body:      "{{1}}",
			fallbacks: nil,
			missing:   []string{"1"},
		},
	}

	for _, tc := range tests {
		got := ValidateVariables(tc.body, tc.fallbacks)
		if !reflect.DeepEqual(got, tc.missing) {
			t.Errorf("ValidateVariables(%q, %v): got %v, want %v", tc.body, tc.fallbacks, got, tc.missing)
		}
	}
}
