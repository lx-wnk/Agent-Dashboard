package channel

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// defaultScreenCols is wide enough that the AskUserQuestion modal never wraps,
// so absolute-column positioning in the captured stream lands exactly where
// the real terminal put it.
const defaultScreenCols = 200

// screen is a minimal VT100/xterm emulator: just enough control-sequence
// handling to reconstruct the visible rows of an AskUserQuestion modal from a
// raw pty byte stream. It is not a general-purpose terminal emulator (no
// scroll regions, no alternate-screen tracking, no wrapping) - only the
// subset of CSI/OSC/C0 behavior actually emitted by Claude Code's TUI.
type screen struct {
	rows               [][]rune
	cols               int
	row, col           int
	savedRow, savedCol int
}

func newScreen(cols int) *screen {
	if cols <= 0 {
		cols = defaultScreenCols
	}
	return &screen{cols: cols}
}

// renderRows replays raw pty bytes through a fresh screen and returns the
// final visible rows, trailing-space-trimmed with trailing empty rows
// dropped.
func renderRows(raw []byte) []string {
	s := newScreen(defaultScreenCols)
	_, _ = s.Write(raw)
	return s.Rows()
}

func (s *screen) Write(p []byte) (int, error) {
	i := 0
	for i < len(p) {
		b := p[i]
		switch {
		case b == 0x1b:
			i += s.handleEscape(p[i:])
		case b == '\r':
			s.col = 0
			i++
		case b == '\n':
			s.row++
			s.ensureRow(s.row)
			i++
		case b == '\b':
			if s.col > 0 {
				s.col--
			}
			i++
		case b < 0x20:
			i++
		default:
			r, size := utf8.DecodeRune(p[i:])
			s.put(r)
			i += size
		}
	}
	return len(p), nil
}

func (s *screen) put(r rune) {
	s.ensureRow(s.row)
	for len(s.rows[s.row]) <= s.col {
		s.rows[s.row] = append(s.rows[s.row], ' ')
	}
	s.rows[s.row][s.col] = r
	s.col++
}

func (s *screen) ensureRow(n int) {
	for len(s.rows) <= n {
		r := make([]rune, s.cols)
		for i := range r {
			r[i] = ' '
		}
		s.rows = append(s.rows, r)
	}
}

// handleEscape parses one escape sequence starting at seq[0] == ESC and
// returns the number of bytes it consumed.
func (s *screen) handleEscape(seq []byte) int {
	if len(seq) < 2 {
		return len(seq)
	}
	switch seq[1] {
	case '[':
		return s.handleCSI(seq)
	case ']':
		return handleOSC(seq)
	case '7':
		s.savedRow, s.savedCol = s.row, s.col
		return 2
	case '8':
		s.row, s.col = s.savedRow, s.savedCol
		return 2
	case '(', ')', '*', '+':
		if len(seq) >= 3 {
			return 3
		}
		return len(seq)
	default:
		return 2
	}
}

// handleCSI parses a CSI sequence (ESC '[' ... final) and applies any cursor
// or erase effect. SGR (color/attribute) and unrecognized/private-mode
// sequences are consumed and ignored - they don't affect the visible grid.
func (s *screen) handleCSI(seq []byte) int {
	j := 2
	for j < len(seq) && (seq[j] < 0x40 || seq[j] > 0x7e) {
		j++
	}
	if j >= len(seq) {
		return len(seq)
	}
	final := seq[j]
	paramStr := string(seq[2:j])
	consumed := j + 1

	if paramStr != "" {
		switch paramStr[0] {
		case '?', '<', '>', '=':
			return consumed
		}
	}

	params := parseCSIParams(paramStr)

	switch final {
	case 'H', 'f':
		row := paramOrDefault(params, 0, 1)
		col := paramOrDefault(params, 1, 1)
		s.row = row - 1
		s.col = col - 1
		if s.row < 0 {
			s.row = 0
		}
		if s.col < 0 {
			s.col = 0
		}
		s.ensureRow(s.row)
	case 'A':
		s.row -= paramOrDefault(params, 0, 1)
		if s.row < 0 {
			s.row = 0
		}
	case 'B':
		s.row += paramOrDefault(params, 0, 1)
		s.ensureRow(s.row)
	case 'C':
		s.col += paramOrDefault(params, 0, 1)
	case 'D':
		s.col -= paramOrDefault(params, 0, 1)
		if s.col < 0 {
			s.col = 0
		}
	case 'G':
		s.col = paramOrDefault(params, 0, 1) - 1
		if s.col < 0 {
			s.col = 0
		}
	case 'J':
		s.eraseDisplay(paramOrDefault(params, 0, 0))
	case 'K':
		s.eraseLine(paramOrDefault(params, 0, 0))
	default:
		// SGR ('m') and everything else (scroll region, device queries, ...)
		// carry no positional effect on the visible grid.
	}
	return consumed
}

func handleOSC(seq []byte) int {
	i := 2
	for i < len(seq) {
		if seq[i] == 0x07 {
			return i + 1
		}
		if seq[i] == 0x1b && i+1 < len(seq) && seq[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return len(seq)
}

func parseCSIParams(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		out[i] = n
	}
	return out
}

func paramOrDefault(params []int, idx, def int) int {
	if idx < len(params) && params[idx] > 0 {
		return params[idx]
	}
	return def
}

func (s *screen) eraseDisplay(mode int) {
	s.ensureRow(s.row)
	switch mode {
	case 1:
		for c := 0; c <= s.col && c < len(s.rows[s.row]); c++ {
			s.rows[s.row][c] = ' '
		}
		for i := 0; i < s.row; i++ {
			clearRow(s.rows[i])
		}
	case 2, 3:
		for i := range s.rows {
			clearRow(s.rows[i])
		}
	default: // 0: cursor to end of screen
		for c := s.col; c < len(s.rows[s.row]); c++ {
			s.rows[s.row][c] = ' '
		}
		for i := s.row + 1; i < len(s.rows); i++ {
			clearRow(s.rows[i])
		}
	}
}

func (s *screen) eraseLine(mode int) {
	s.ensureRow(s.row)
	row := s.rows[s.row]
	switch mode {
	case 1:
		for c := 0; c <= s.col && c < len(row); c++ {
			row[c] = ' '
		}
	case 2:
		clearRow(row)
	default: // 0: cursor to end of line
		for c := s.col; c < len(row); c++ {
			row[c] = ' '
		}
	}
}

func clearRow(row []rune) {
	for i := range row {
		row[i] = ' '
	}
}

// Rows returns the visible grid, each row right-trimmed of trailing spaces
// and with trailing all-empty rows dropped.
func (s *screen) Rows() []string {
	out := make([]string, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, strings.TrimRight(string(r), " "))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}
