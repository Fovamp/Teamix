// Package serve exposes HTTP endpoints. This file embeds the Vue3 frontend build.
package serve

import (
	"embed"
)

//go:embed webdist/index.html
var webIndexHTML []byte

//go:embed webdist/assets
var webAssets embed.FS
