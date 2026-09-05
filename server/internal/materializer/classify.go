package materializer

import (
	"errors"
	"fmt"
	"os"
)

// Outcome is what happened, or would happen, at one target.
//
// Three of these are cmd/serve/hooks.go's outcome set (hooks.go:265-271),
// which this component adopts rather than reinvents; created, conflict,
// foreign and unsupported are the cases a per-file artefact adds to a
// per-settings-key one.
type Outcome string

const (
	// OutcomeCreated means no file was at the target path.
	OutcomeCreated Outcome = "created"
	// OutcomeUnchanged means our file already holds the resource's content.
	OutcomeUnchanged Outcome = "unchanged"
	// OutcomeRepaired means our file drifted from the resource and the
	// database is the truth for a file we own.
	OutcomeRepaired Outcome = "repaired"
	// OutcomeConflict means a human edited a file we wrote. The run stops for
	// that resource: no merge, no overwrite, and no retry that would overwrite
	// it later.
	OutcomeConflict Outcome = "conflict"
	// OutcomeForeign means the file at the target was not written by this
	// node. It is never touched.
	OutcomeForeign Outcome = "foreign"
	// OutcomeUnsupported means the runtime has no skill format. A recorded
	// no-op, never a silent gap and never a fabricated format.
	OutcomeUnsupported Outcome = "unsupported"
	// OutcomeFailed means this target could not be processed. Other targets
	// still proceed, and the run reports itself as partial.
	OutcomeFailed Outcome = "failed"
)

// Classify decides what would happen at path. It reads the filesystem and
// writes nothing — every write decision in this package flows through it
// first, so a caller that only wants a report calls exactly the same code the
// caller that writes does.
//
// recordedHash is the SHA-256 of the bytes a previous run wrote at this
// target, or "" when this node has never written these bytes. The empty string
// deliberately covers two situations at once — never materialized here, and
// materialized-as-foreign — because the answer is the same for both: a file
// that is present may not be overwritten, and a path that is free may be
// written. A foreign file the user deleted is a path this node is now entitled
// to.
//
// On why the mtime does not appear here, see the plan's Task 4 design note:
// spec §6 concedes that a whole-second mtime misses a same-second edit and
// that the content hash is what covers it, so the hash decides every case and
// a stored mtime would be a field nothing reads.
func Classify(path string, want []byte, recordedHash string) (Outcome, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return OutcomeCreated, nil
	}
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	// Lstat, and IsRegular: a symlink or anything else at the target is not
	// something this node wrote and not something it will write through. The
	// read side refuses the same shape when enumerating
	// (cmdscope/enumerate.go:378-382).
	if !info.Mode().IsRegular() {
		return OutcomeForeign, nil
	}
	if recordedHash == "" {
		return OutcomeForeign, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	switch have := HashBytes(data); {
	case have != recordedHash:
		return OutcomeConflict, nil
	case have == HashBytes(want):
		return OutcomeUnchanged, nil
	default:
		return OutcomeRepaired, nil
	}
}
