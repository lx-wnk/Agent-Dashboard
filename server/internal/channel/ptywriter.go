package channel

import (
	"io"
	"time"
)

// ptyWriter serializes every write to the pty master through a single goroutine.
//
// The pty has several independent writers: POST /message, POST /keys, one input
// pump per WebSocket client, and — in the interactive host — io.Copy from the
// real terminal's stdin. A single-shot write was implicitly atomic, but an
// injected prompt is now two writes separated by injectSubmitDelay, and anything
// another writer emits inside that window would land in the middle of the
// injected text and be submitted with it.
//
// Writers hand over a job and block until it is done, so the buffers they pass
// stay valid for the lifetime of the write and no copy is needed. The goroutine
// lives as long as the process: a broker process IS one session, and shutting
// the channel down while a handler still holds a reference would trade a
// harmless goroutine for a panic.
type ptyWriter struct {
	// raw is the underlying pty master, kept for operations that are not writes
	// (applyResize type-asserts it to *os.File for the TIOCSWINSZ ioctl).
	raw  io.Writer
	jobs chan ptyWriteJob
}

type ptyWriteJob struct {
	parts [][]byte
	gap   time.Duration
	errc  chan error
}

func newPtyWriter(raw io.Writer) *ptyWriter {
	p := &ptyWriter{raw: raw, jobs: make(chan ptyWriteJob)}
	go func() {
		for job := range p.jobs {
			var err error
			for i, part := range job.parts {
				if i > 0 && job.gap > 0 {
					time.Sleep(job.gap)
				}
				if _, err = p.raw.Write(part); err != nil {
					break
				}
			}
			job.errc <- err
		}
	}()
	return p
}

// Write satisfies io.Writer so the WebSocket input pump and the stdin copy can
// keep using it directly. Each call is one indivisible job.
func (p *ptyWriter) Write(b []byte) (int, error) {
	if err := p.WriteParts(0, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

// WriteParts writes parts in order, pausing gap between them, with no other
// writer able to interleave. A failing part aborts the rest and returns its
// error — the caller decides what a half-written sequence means.
func (p *ptyWriter) WriteParts(gap time.Duration, parts ...[]byte) error {
	errc := make(chan error, 1)
	p.jobs <- ptyWriteJob{parts: parts, gap: gap, errc: errc}
	return <-errc
}
