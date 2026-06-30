package checkpoint

import (
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultDebounce = 2 * time.Second

// CheckpointerOptions configures a Checkpointer.
type CheckpointerOptions struct {
	// DebounceInterval is the quiet-period before a snapshot fires. Default: 2s.
	DebounceInterval time.Duration
	// OnSnapshot is called when the debounce fires. Production wires the full
	// Snapshot+DB path; tests inject a lightweight callback.
	OnSnapshot func(taskID, worktreePath string)
}

// entry holds the per-task watcher state.
type entry struct {
	watcher *fsnotify.Watcher
	cancel  chan struct{}
}

// Checkpointer manages one fsnotify watcher per active task worktree.
type Checkpointer struct {
	opts    CheckpointerOptions
	mu      sync.Mutex
	entries map[string]*entry
}

// NewCheckpointer creates a Checkpointer with the given options.
func NewCheckpointer(opts CheckpointerOptions) *Checkpointer {
	if opts.DebounceInterval <= 0 {
		opts.DebounceInterval = defaultDebounce
	}
	return &Checkpointer{opts: opts, entries: make(map[string]*entry)}
}

// Start launches (or restarts) a watcher for taskID over worktreePath.
// Calling Start for a task that already has a watcher stops the old one first.
func (c *Checkpointer) Start(taskID, worktreePath string) {
	c.Stop(taskID)

	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("checkpointer: create watcher failed", "taskID", taskID, "err", err)
		return
	}
	if err := addRecursive(w, worktreePath); err != nil {
		slog.Warn("checkpointer: add watch failed", "taskID", taskID, "err", err)
		_ = w.Close()
		return
	}

	cancel := make(chan struct{})
	c.mu.Lock()
	c.entries[taskID] = &entry{watcher: w, cancel: cancel}
	c.mu.Unlock()

	go c.debounceLoop(taskID, worktreePath, w, cancel)
}

// Stop tears down the watcher for taskID (no-op if not running).
func (c *Checkpointer) Stop(taskID string) {
	c.mu.Lock()
	e, ok := c.entries[taskID]
	if ok {
		delete(c.entries, taskID)
	}
	c.mu.Unlock()
	if ok {
		close(e.cancel)
		_ = e.watcher.Close()
	}
}

// StopAll stops all active watchers (called at server shutdown).
func (c *Checkpointer) StopAll() {
	c.mu.Lock()
	ids := make([]string, 0, len(c.entries))
	for id := range c.entries {
		ids = append(ids, id)
	}
	c.mu.Unlock()
	for _, id := range ids {
		c.Stop(id)
	}
}

// debounceLoop resets a timer on each meaningful FS event and fires OnSnapshot
// once the worktree has been quiet for DebounceInterval.
func (c *Checkpointer) debounceLoop(taskID, worktreePath string, w *fsnotify.Watcher, cancel chan struct{}) {
	timer := time.NewTimer(c.opts.DebounceInterval)
	timer.Stop()
	for {
		select {
		case <-cancel:
			timer.Stop()
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if shouldIgnore(ev.Name) {
				continue
			}
			// Register newly-created subdirectories so the watch stays recursive.
			if ev.Has(fsnotify.Create) {
				_ = addRecursive(w, ev.Name)
			}
			resetTimer(timer, c.opts.DebounceInterval)
		case <-w.Errors:
			// best-effort; ignore and continue
		case <-timer.C:
			if c.opts.OnSnapshot != nil {
				c.opts.OnSnapshot(taskID, worktreePath)
			}
		}
	}
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

// shouldIgnore returns true for paths that must never trigger a snapshot.
func shouldIgnore(path string) bool {
	for _, seg := range []string{"/.git/", "/node_modules/", "/dist/"} {
		if strings.Contains(path, seg) {
			return true
		}
	}
	base := filepath.Base(path)
	return base == ".git" || base == "node_modules" || base == "dist"
}

// addRecursive walks path and registers every non-ignored directory with the
// watcher. fsnotify v1.9.0 has no native recursive mode, so subdirectories are
// added individually.
func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		if !d.IsDir() {
			return nil
		}
		if shouldIgnore(p) {
			return filepath.SkipDir
		}
		if addErr := w.Add(p); addErr != nil {
			slog.Debug("checkpointer: watch add failed", "path", p, "err", addErr)
		}
		return nil
	})
}
