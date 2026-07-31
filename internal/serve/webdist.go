// Package serve embeds the Vue3 frontend build as the main web UI.
package serve

import (
	"embed"
)

//go:embed webdist-v3/index.html
var v3IndexHTML []byte

//go:embed webdist-v3/assets
var v3Assets embed.FS
