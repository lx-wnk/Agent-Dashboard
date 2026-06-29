package plugin

import (
	"testing"
)

func TestSanitizeSettingKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"apiKey", "APIKEY"},
		{"api-key", "API_KEY"},
		{"my.setting", "MY_SETTING"},
		{"FOO_BAR", "FOO_BAR"},
		{"a b c", "A_B_C"},
		{"x123", "X123"},
		{"ALREADY_UPPER", "ALREADY_UPPER"},
	}
	for _, tc := range cases {
		got := sanitizeSettingKey(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeSettingKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
