// Package ui exposes the production frontend filesystem.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var assets embed.FS

func Files() fs.FS {
	f, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return f
}
