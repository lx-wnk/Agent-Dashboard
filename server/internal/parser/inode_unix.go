//go:build darwin || linux

package parser

import (
	"io/fs"
	"syscall"
)

// inodeOf extracts the inode number from a FileInfo.
// Returns 0 if the underlying stat type is not available (e.g., non-unix FS).
func inodeOf(info fs.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}
