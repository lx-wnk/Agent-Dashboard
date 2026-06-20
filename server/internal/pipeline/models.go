package pipeline

// allowedModelIDs is the server-side mirror of AVAILABLE_MODELS in src/utils/models.ts.
// Keep in parity with that file — Go cannot import TypeScript.
var allowedModelIDs = map[string]bool{
	"claude-opus-4-6":   true,
	"claude-sonnet-4-6": true,
	"claude-haiku-4-5":  true,
}

// IsValidModel reports whether id is a known, supported Claude model.
func IsValidModel(id string) bool {
	return allowedModelIDs[id]
}
