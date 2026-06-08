// Package frontend embeds the compiled Vue SPA for serving.
package frontend

import "embed"

// Embedded holds the compiled Vue SPA from the dist/ directory.
// In production, dist/ contains the pnpm build output.
// In development, dist/ holds only .gitkeep; Vite runs separately on :5173.
//
// The all: prefix embeds dot-prefixed files too, so a freshly cloned dist/
// containing only .gitkeep still satisfies the embed and compiles. Without
// all:, an empty-but-for-.gitkeep dist/ would fail with "no matching files".
//
//go:embed all:dist
var Embedded embed.FS
