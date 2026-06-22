package provider

import "embed"

//go:embed providers/*.yaml
var builtinFS embed.FS
