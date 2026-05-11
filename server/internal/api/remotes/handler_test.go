package remotes

import "testing"

func TestIsSafeRemoteURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"valid external https", "https://example.com/api", true},
		{"valid external http", "http://remote.example.org", true},
		{"localhost", "http://localhost/api", false},
		{"127.0.0.1", "http://127.0.0.1/api", false},
		{"ipv6 loopback ::1", "http://[::1]/api", false},
		{"0.0.0.0", "http://0.0.0.0/api", false},
		{"127.x.x.x loopback", "http://127.5.6.7/api", false},
		{"169.254 link-local", "http://169.254.1.2/api", false},
		{"ftp protocol", "ftp://example.com/api", false},
		{"empty string", "", false},
		{"no scheme", "example.com/api", false},
		{"RFC-1918 10.x", "http://10.0.0.1", false},
		{"RFC-1918 192.168.x.x", "http://192.168.1.1", false},
		{"RFC-1918 172.16.x.x", "http://172.16.0.1", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isSafeRemoteURL(tc.url)
			if got != tc.want {
				t.Errorf("isSafeRemoteURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}
