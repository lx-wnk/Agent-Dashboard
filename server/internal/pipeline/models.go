package pipeline

// allowedModelIDs is the server-side mirror of AVAILABLE_MODELS in src/utils/models.ts.
// Keep in parity with that file — Go cannot import TypeScript; models_parity_test.go fails on drift.
var allowedModelIDs = map[string]bool{
	"claude-fable-5-1":  true,
	"claude-opus-5":     true,
	"claude-sonnet-5":   true,
	"claude-opus-4-8":   true,
	"claude-opus-4-6":   true,
	"claude-sonnet-4-6": true,
	"claude-haiku-4-5":  true,
}

// IsValidModel reports whether id is a known, supported Claude model.
func IsValidModel(id string) bool {
	return allowedModelIDs[id]
}
