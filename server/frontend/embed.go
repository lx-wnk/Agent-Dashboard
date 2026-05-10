// Package frontend embeds the compiled Vue SPA for serving.
package frontend

import "embed"

// Embedded holds the compiled Vue SPA from the dist/ directory.
// In production, dist/ contains the pnpm build output.
// In development, a placeholder is used; Vite runs separately on :5173.
//
//go:embed dist
var Embedded embed.FS
