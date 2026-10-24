package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var embedded embed.FS

var Files http.FileSystem

func init() {
	root, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	Files = http.FS(root)
}
