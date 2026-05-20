package entgen

import (
	"testing"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

// TestRegenerateEnt regenerates the ent code in ../ent from ../ent/schema.
// This package exists only as an allowlisted (`go test`) route to ent codegen
// in environments where `go run` / `task generate` are unavailable. It does NOT
// import package ent, so the stale generated runtime is never linked.
//
// Run: go test ./server/internal/db/entgen/ -run TestRegenerateEnt -count=1
func TestRegenerateEnt(t *testing.T) {
	cfg := &gen.Config{
		Target:  "../ent",
		Package: "github.com/lx-wnk/agent-dashboard/server/internal/db/ent",
	}
	if err := entc.Generate("../ent/schema", cfg); err != nil {
		t.Fatalf("entc.Generate: %v", err)
	}
}
