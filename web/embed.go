// Package web embeds the built frontend (Vite output in dist/) into the Go
// binary. During local development without a frontend build, dist/ contains a
// placeholder index.html; the Docker build replaces it with the real bundle.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded dist/ directory as a filesystem rooted at dist.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
