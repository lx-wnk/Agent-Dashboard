package rawrepo

import (
	"testing"
)

func TestSanitizeFTSQuery(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "single word",
			input: "hello",
			want:  `"hello"*`,
		},
		{
			name:  "two words",
			input: "hello world",
			want:  `"hello"* "world"*`,
		},
		{
			name:  "internal double quote is doubled",
			input: `he"llo`,
			want:  `"he""llo"*`,
		},
		{
			name:  "multiple internal quotes",
			input: `a"b"c`,
			want:  `"a""b""c"*`,
		},
		{
			name:  "leading/trailing whitespace",
			input: "  foo  bar  ",
			want:  `"foo"* "bar"*`,
		},
		{
			name:  "tab separator",
			input: "foo\tbar",
			want:  `"foo"* "bar"*`,
		},
		{
			name:  "punctuation only",
			input: "***",
			want:  `"***"*`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeFTSQuery(tc.input)
			if got != tc.want {
				t.Errorf("SanitizeFTSQuery(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
