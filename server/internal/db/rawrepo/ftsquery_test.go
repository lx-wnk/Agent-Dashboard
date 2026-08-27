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
		// The single-token cases above prove escaping and prefix-wrapping in
		// isolation; they don't prove each token is wrapped and escaped
		// independently rather than, say, the whole input being escaped once
		// and then split. These multi-token cases exercise that: every token
		// gets its own independently-doubled quotes and its own wrapper,
		// regardless of what its neighbors contain.
		{
			name:  "multiple tokens, one with an embedded quote",
			input: `he"llo world`,
			want:  `"he""llo"* "world"*`,
		},
		{
			name:  "multiple tokens, one punctuation-only",
			input: "hello ***",
			want:  `"hello"* "***"*`,
		},
		{
			name:  "mix of a quoted token and a punctuation-only token",
			input: `he"llo *** wor"ld`,
			want:  `"he""llo"* "***"* "wor""ld"*`,
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
