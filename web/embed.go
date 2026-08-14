// SPDX-License-Identifier: AGPL-3.0-only

// Package web embeds the built frontend into the mikroview binary so it
// ships as a single self-contained executable/image.
//
// dist/ is gitignored except for an empty committed .gitkeep, which
// exists only because go:embed requires its pattern to match at least
// one file at compile time -- the `all:` prefix is what makes a dotfile
// count. Running `npm run build` in frontend/ and copying its output
// here (see the Makefile's `build` target) fills the directory with the
// real UI before the final `go build`.
//
// Nothing built is committed. A real index.html used to be, as the
// placeholder, and it went wrong in both available directions (#353):
// every build overwrote it, so unrelated changes silently acquired it,
// and the committed copy named asset filenames that the tracked source
// no longer produced. It also made a frontend-less build look like it
// had a UI -- serving a page whose scripts 404 -- which HasUI now lets
// the caller report instead.
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

// HasUI reports whether a frontend was actually built into this binary.
//
// `go build` on a fresh clone succeeds with nothing in dist/ but
// .gitkeep, which is the point of .gitkeep -- so the binary is valid and
// the UI simply is not there. Without this the only symptom is a 404 on
// every page load, which reads as a broken install rather than a build
// step that was skipped.
func HasUI() bool {
	dist, err := DistFS()
	if err != nil {
		return false
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return false
	}
	return true
}
