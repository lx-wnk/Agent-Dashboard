package restart

import (
	"slices"
	"testing"
)

// A rebuild that does not stamp the revision leaves the new binary reporting
// "dev" while the source reports a revision, so the staleness banner the
// rebuild exists to clear would be stuck on forever.
func TestBuildArgsStampsTheRevision(t *testing.T) {
	got := buildArgs("/tmp/bin", "v1.2.3")
	want := []string{"build", "-o", "/tmp/bin", "-ldflags", "-X main.version=v1.2.3", "./cmd/serve/..."}
	if !slices.Equal(got, want) {
		t.Fatalf("buildArgs = %q, want %q", got, want)
	}
}

// An unknown revision must stamp nothing: "-X main.version=" would read as a
// real, empty version rather than as "unstamped".
func TestBuildArgsOmitsTheStampWhenTheRevisionIsUnknown(t *testing.T) {
	got := buildArgs("/tmp/bin", "")
	want := []string{"build", "-o", "/tmp/bin", "./cmd/serve/..."}
	if !slices.Equal(got, want) {
		t.Fatalf("buildArgs = %q, want %q", got, want)
	}
}
