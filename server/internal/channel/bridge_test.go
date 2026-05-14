package channel

import "testing"

func TestIsLoopbackURL(t *testing.T) {
	cases := []struct {
		url      string
		loopback bool
	}{
		{"http://127.0.0.1:13120", true},
		{"http://127.0.0.1:13120/api/mcp", true},
		{"http://localhost:13120", true},
		{"http://[::1]:8080", true},
		{"http://192.168.1.5:13120", false},
		{"http://example.com/api/mcp", false},
		{"https://external.host:443", false},
		{"not-a-url", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isLoopbackURL(tc.url)
		if got != tc.loopback {
			t.Errorf("isLoopbackURL(%q) = %v, want %v", tc.url, got, tc.loopback)
		}
	}
}
