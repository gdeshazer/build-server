package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
)

var (
	pageTmpl    *template.Template
	partialTmpl *template.Template
)

func dictFunc(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict requires even number of args")
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		k, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict key must be string")
		}
		m[k] = pairs[i+1]
	}
	return m, nil
}

func InitTemplates(fsys fs.FS) error {
	funcMap := template.FuncMap{
		"dict": dictFunc,
	}

	var err error
	pageTmpl, err = template.New("").Funcs(funcMap).ParseFS(
		fsys,
		"templates/layout.html",
		"templates/index.html",
		"templates/partials/repo_row.html",
		"templates/partials/repo_list.html",
		"templates/partials/build_log.html",
	)
	if err != nil {
		return err
	}

	partialTmpl, err = template.New("").Funcs(funcMap).ParseFS(
		fsys,
		"templates/partials/repo_row.html",
		"templates/partials/repo_list.html",
		"templates/partials/build_log.html",
	)
	return err
}

func renderPage(w http.ResponseWriter, name string, data any) {
	base := filepath.Base(name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.ExecuteTemplate(w, base, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func renderPartial(w http.ResponseWriter, name string, data any) {
	base := filepath.Base(name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := partialTmpl.ExecuteTemplate(w, base, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}
