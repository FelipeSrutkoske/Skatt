package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedDist embed.FS

var DistFS fs.FS = mustSubDist()

func mustSubDist() fs.FS {
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
