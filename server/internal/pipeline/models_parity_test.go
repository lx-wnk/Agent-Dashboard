package pipeline

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The SPA offers AVAILABLE_MODELS as stage-model choices and the pipeline
// config API rejects anything outside allowedModelIDs, so a model present on
// only one side is either unselectable or selectable-then-refused with a 400.
func TestAllowedModelIDs_MatchFrontendList(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "src", "utils", "models.ts"))
	if err != nil {
		t.Fatalf("read src/utils/models.ts: %v", err)
	}
	block := regexp.MustCompile(`(?s)AVAILABLE_MODELS = \[(.*?)\] as const`).FindSubmatch(src)
	if block == nil {
		t.Fatal("AVAILABLE_MODELS array not found in src/utils/models.ts")
	}
	var frontend []string
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(block[1], -1) {
		frontend = append(frontend, string(m[1]))
	}

	var server []string
	for id := range allowedModelIDs {
		server = append(server, id)
	}
	sort.Strings(frontend)
	sort.Strings(server)

	if strings.Join(frontend, ",") != strings.Join(server, ",") {
		t.Fatalf("model lists drifted:\n  src/utils/models.ts: %v\n  allowedModelIDs:     %v", frontend, server)
	}
}
