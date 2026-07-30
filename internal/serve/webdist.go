package serve

import (
    _ "embed"
)

//go:embed webdist/index.html
var webIndexHTML []byte
