// SPDX-License-Identifier: AGPL-3.0-only

// Package web embeds the built frontend into the mikroview binary so it
// ships as a single self-contained executable/image.
//
// dist/ is gitignored except for a committed placeholder index.html
// (index.html itself, not the directory, since go:embed requires the
// pattern to match at least one file at compile time). Running
// `npm run build` in frontend/ and copying its output here (see the
// Makefile's `build` target) overwrites the placeholder with the real UI
// before the final `go build`.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded frontend build, rooted at dist/ so paths
// match what an http.FileServer expects ("index.html", not
// "dist/index.html").
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
