// Package konfirm embeds the frontend into the server binary.
//
// This lives at the module root because go:embed cannot reference parent
// directories, and the frontend must be reachable from a package that sits
// above it.
//
// Embedding is a deployment decision, not a style one. The alternative — a
// separate frontend host — means CORS configuration, a second deploy target,
// two sets of environment variables, and the standing possibility of the
// frontend and API drifting to different versions. One binary cannot drift
// from itself.
package konfirm

import (
	"embed"
	"io/fs"
)

//go:embed all:frontend
var frontendFS embed.FS

// Frontend returns the embedded frontend rooted at the frontend directory,
// so "/css/app.css" resolves rather than "/frontend/css/app.css".
func Frontend() fs.FS {
	sub, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		// Unreachable: the embed directive is resolved at compile time, so a
		// failure here means the binary was built wrong, not that input was bad.
		panic("konfirm: frontend embed is broken: " + err.Error())
	}
	return sub
}
