// Package frontend owns the static browser application source.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed index.html application content images styles
var embeddedAssets embed.FS

// APIOriginMarker is replaced exactly once when the local application index is served.
const APIOriginMarker = "__DOWNLOAD_YOUR_DATA_API_ORIGIN__"

// Assets returns the complete browser application filesystem.
func Assets() fs.FS {
	return embeddedAssets
}
