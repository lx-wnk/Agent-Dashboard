package materializer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// skillFileMode is owner-only for the same reason cmd/serve/hooks.go writes
// settings.json 0600 and its directory 0700: this is the config directory that
// also holds session transcripts and the hooks secret.
const (
	skillFileMode = 0o600
	skillDirMode  = 0o700
)

// Apply writes want to path, atomically.
//
// t bounds the symlink refusal: every directory component below t.Root must be
// a real directory, while t.Root itself may be a symlink — a ~/.claude linked
// into ~/.claude-personal is an ordinary dotfiles layout, and this project's
// own author runs one.
func Apply(t Target, path string, want []byte) error {
	dir := filepath.Dir(path)
	if err := refuseSymlinkBelow(t.Root, dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, skillDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return atomicWrite(path, want, skillFileMode)
}

// refuseSymlinkBelow walks root down to dir and refuses any component that is
// a symlink or not a directory. A symlinked skills/ or skills/<slug>/ would
// redirect the write outside the configured root — the attack the enumeration
// side already refuses on read (cmdscope/enumerate.go:365-390).
func refuseSymlinkBelow(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%s is not below %s", dir, root)
	}
	cur := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		cur = filepath.Join(cur, part)
		info, serr := os.Lstat(cur)
		if errors.Is(serr, os.ErrNotExist) {
			// Nothing exists from here down; MkdirAll creates real directories.
			return nil
		}
		if serr != nil {
			return fmt.Errorf("stat %s: %w", cur, serr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", cur)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", cur)
		}
	}
	return nil
}

// atomicWrite replaces path with data: temp file in the target's own
// directory, Sync, Close, Chmod, Rename.
//
// This is hookscript.writeExecutable's shape (hookscript.go:44-73), not
// api/config/file.go's (file.go:187-210) — the latter omits tmp.Sync(), and a
// skill file that survives a rename but not a power loss is not worth the
// saved syscall. The deferred Remove is a no-op once the rename succeeds and
// is what stops a failed rename from leaving a stray temp file behind.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".materialize-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file next to %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
