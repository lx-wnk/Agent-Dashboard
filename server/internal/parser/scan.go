package parser

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
)

// ErrStopScan, when returned from a ScanJSONLLines callback, stops the scan
// without surfacing an error.
var ErrStopScan = errors.New("stop scan")

// maxLineBytes bounds the size of a single JSONL line ScanJSONLLines will
// hand to its callback. A line beyond this is skipped (not decoded, not
// invoked) rather than aborting the whole scan.
const maxLineBytes = 4 * 1024 * 1024

// tailReadCloser wraps a LimitReader over an *os.File so Close reaches the file.
type tailReadCloser struct {
	io.Reader
	f *os.File
}

func (t tailReadCloser) Close() error { return t.f.Close() }

// OpenJSONLReader opens path for reading. When maxBytes > 0 and the file is
// larger, it seeks to the last maxBytes and returns a reader bounded to that
// tail; maxBytes == 0 reads the whole file. Caller must Close the result.
func OpenJSONLReader(path string, maxBytes int64) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		return f, nil
	}
	info, err := f.Stat()
	if err != nil {
		f.Close() //nolint:errcheck
		return nil, err
	}
	if info.Size() <= maxBytes {
		return f, nil
	}
	if _, err := f.Seek(info.Size()-maxBytes, io.SeekStart); err != nil {
		f.Close() //nolint:errcheck
		return nil, err
	}
	return tailReadCloser{Reader: io.LimitReader(f, maxBytes), f: f}, nil
}

// ScanJSONLLines scans r line by line, trimming whitespace and skipping empty
// lines, invoking fn for each non-empty line. A line longer than maxLineBytes
// is skipped (fn is not invoked) and logged at Info, rather than aborting the
// scan — this keeps a single oversized JSONL line from silently truncating the
// token/message totals for the rest of the file. A fn returning ErrStopScan
// stops the scan with no error; any other fn error (or a read error)
// propagates. The callback receives a slice valid only for the duration of
// the call — do not retain it.
func ScanJSONLLines(r io.Reader, fn func(line []byte) error) error {
	br := bufio.NewReaderSize(r, 256*1024)
	for {
		raw, readErr := br.ReadBytes('\n')
		if len(raw) > 0 {
			if err := scanJSONLLine(raw, fn); err != nil {
				if errors.Is(err, ErrStopScan) {
					return nil
				}
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

// scanJSONLLine handles one line already read from the underlying reader
// (still carrying its trailing newline, if any).
func scanJSONLLine(raw []byte, fn func(line []byte) error) error {
	if len(raw) > maxLineBytes {
		slog.Info("parser: skipping over-long JSONL line", "bytes", len(raw))
		return nil
	}
	line := bytes.TrimSpace(raw)
	if len(line) == 0 {
		return nil
	}
	return fn(line)
}
