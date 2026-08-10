package validation

import (
	"strconv"
	"unicode/utf8"
)

// MaxProjectNameLen caps a project name, in runes. Matches the task.title cap so
// the two sibling headline fields agree.
const MaxProjectNameLen = 200

// MaxProjectDescriptionLen caps a project description, in runes.
// Mirrors MAX_DESCRIPTION_CHARS in src/utils/validation.ts.
const MaxProjectDescriptionLen = 10_000

// ProjectNameLengthMessage is the human-readable description of MaxProjectNameLen.
var ProjectNameLengthMessage = "name must be at most " + strconv.Itoa(MaxProjectNameLen) + " characters"

// ProjectDescriptionLengthMessage is the human-readable description of MaxProjectDescriptionLen.
var ProjectDescriptionLengthMessage = "description must be at most " + strconv.Itoa(MaxProjectDescriptionLen) + " characters"

// IsValidProjectName reports whether name fits MaxProjectNameLen.
// Canonical rule for every project writer — import it instead of counting locally.
func IsValidProjectName(name string) bool {
	return utf8.RuneCountInString(name) <= MaxProjectNameLen
}

// IsValidProjectDescription reports whether description fits MaxProjectDescriptionLen.
func IsValidProjectDescription(description string) bool {
	return utf8.RuneCountInString(description) <= MaxProjectDescriptionLen
}
