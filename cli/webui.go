package main

import (
	"embed"
	"io/fs"
)

// The Svelte frontend in web/ is built into web/dist (make build) and embedded,
// so the binary needs no Node or external assets at runtime. web/dist/.gitkeep
// keeps the embed compiling in a fresh checkout before the UI has been built;
// the server then answers with a "web UI not built" error instead of a page.
//
//go:embed all:web/dist
var webDistFS embed.FS

func webDist() (fs.FS, error) {
	return fs.Sub(webDistFS, "web/dist")
}
