// Package platform provides runtime platform detection.
package platform

import "runtime"

// IsLinux is true when running on Linux. Used to switch between
// /proc/<pid>/cwd (Linux) and lsof (macOS) for process CWD resolution.
var IsLinux = runtime.GOOS == "linux"
