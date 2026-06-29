package plugin

import (
	"context"
	"strings"
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

func TestAppendSettingsEnv_CollidingKeysStillInject(t *testing.T) {
	r := New("")
	r.SetSettingsProvider(func(_ context.Context, _ string) (map[string]string, error) {
		// "api-key" and "api.key" both sanitize to PLUGIN_SETTING_API_KEY.
		return map[string]string{"api-key": "v1", "api.key": "v2", "plain": "p"}, nil
	})

	env := r.appendSettingsEnv(context.Background(), nil, "p1")

	var apiKeyVars, plainVars int
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, "PLUGIN_SETTING_API_KEY="):
			apiKeyVars++
		case strings.HasPrefix(kv, "PLUGIN_SETTING_PLAIN="):
			plainVars++
		}
	}
	if plainVars != 1 {
		t.Errorf("non-colliding key not injected: got %d PLAIN vars", plainVars)
	}
	if apiKeyVars == 0 {
		t.Error("colliding keys produced no API_KEY var")
	}
}
