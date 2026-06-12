package main

import (
	"embed"
	"io/fs"

	"hookwatch/server"
)

//go:embed frontend/dist/*
var frontendFS embed.FS

func main() {
	sub, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		server.Start(nil)
		return
	}
	server.Start(sub)
}
