package assets

import "embed"

// FS contains runtime UI assets that should be embedded into the executable.
//
//go:embed icons/*.png fonts/* *.ico
var FS embed.FS
