//go:build embed

package main

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticContent embed.FS

//go:embed templates/*
var templateContent embed.FS

var staticHandler http.Handler

func init() {
	sub, err := fs.Sub(staticContent, "static")
	if err != nil {
		panic(err)
	}
	staticHandler = http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}

func loadTemplates() (*template.Template, *template.Template, error) {
	pageBytes, err := templateContent.ReadFile("templates/page.html")
	if err != nil {
		return nil, nil, err
	}
	page, err := template.New("page.html").Parse(string(pageBytes))
	if err != nil {
		return nil, nil, err
	}
	blockBytes, err := templateContent.ReadFile("templates/cmd_block.html")
	if err != nil {
		return nil, nil, err
	}
	block, err := template.New("cmd_block.html").Parse(string(blockBytes))
	if err != nil {
		return nil, nil, err
	}
	return page, block, nil
}
