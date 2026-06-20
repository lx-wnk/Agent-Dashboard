package parser

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
)

// ErrStopScan, when returned from a ScanJSONLLines callback, stops the scan
// without surfacing an error.
var ErrStopScan = errors.New("stop scan")

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
// lines, invoking fn for each non-empty line. A fn returning ErrStopScan stops
// the scan with no error; any other fn error (or a scanner error) propagates.
// The callback receives a slice valid only for the duration of the call — do not retain it.
func ScanJSONLLines(r io.Reader, fn func(line []byte) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			if errors.Is(err, ErrStopScan) {
				return nil
			}
			return err
		}
	}
	return scanner.Err()
}
