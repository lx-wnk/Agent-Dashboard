package claudeconfig

import (
	"context"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchDebounce is how long the config has to be quiet before a change is
// reported. Exported so a test does not have to wait it out.
var WatchDebounce = 300 * time.Millisecond

// Watch reports a debounced change to Claude's config until ctx is done. It
// watches the config's directory, not the file: an editor that replaces the
// file would break a watch on the file itself. A missing file is not an error.
func Watch(ctx context.Context, onChange func()) error {
	path, err := JSONPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := w.Add(dir); err != nil {
		w.Close()
		return err
	}

	go func() {
		defer w.Close()
		timer := time.NewTimer(WatchDebounce)
		timer.Stop()
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Base(ev.Name) != base {
					continue
				}
				resetTimer(timer, WatchDebounce)
			case <-w.Errors:
				// best-effort; ignore and continue
			case <-timer.C:
				onChange()
			}
		}
	}()

	return nil
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}
