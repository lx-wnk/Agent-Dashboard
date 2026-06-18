package parser

// Test-only accessors for the candidate-cache stat counter. Defined in an
// internal test file so the external parser_test package can assert how many
// full statSessionFiles directory scans the candidate cache actually triggers.

// StatSessionFilesCalls returns the number of full statSessionFiles scans so far.
func StatSessionFilesCalls() int64 { return statSessionFilesCalls.Load() }

// ResetStatSessionFilesCalls zeroes the scan counter and clears the candidate
// cache so each test starts from a clean slate.
func ResetStatSessionFilesCalls() {
	statSessionFilesCalls.Store(0)
	candidateCacheMu.Lock()
	candidateCache = make(map[string]*candidateCacheEntry)
	candidateCacheMu.Unlock()
}
