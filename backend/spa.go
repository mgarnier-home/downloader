package main

import (
	"io/fs"
	"net/http"
	"path"
)

// spaHandler sert le fichier demandé s'il existe, sinon index.html (routage côté client).
func spaHandler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(r.URL.Path)[1:]
		if name != "" {
			if info, err := fs.Stat(fsys, name); err != nil || info.IsDir() {
				r.URL.Path = "/"
			}
		}
		files.ServeHTTP(w, r)
	})
}
