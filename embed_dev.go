//go:build !embed

package main

import (
	"html/template"
	"net/http"
)

var staticHandler = http.StripPrefix("/static/", http.FileServer(http.Dir("static")))

func loadTemplates() (*template.Template, *template.Template, error) {
	page, err := template.ParseFiles("templates/page.html")
	if err != nil {
		return nil, nil, err
	}
	block, err := template.ParseFiles("templates/cmd_block.html")
	if err != nil {
		return nil, nil, err
	}
	return page, block, nil
}
