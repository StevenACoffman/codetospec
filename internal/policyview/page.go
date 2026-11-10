package policyview

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"

	"github.com/StevenACoffman/codetospec/internal/d2"
)

// Files are the views' files: index.html, the access matrix, and each
// decision diagram's (pictures, as d2.Renderer.Draw returned them for
// v.Names), which the page links to: an SVG when drawn, else its source.
//
// Ensures: deterministic for given views and pictures.
func (v *Views) Files(pictures []d2.Picture) (map[string][]byte, error) {
	files := map[string][]byte{}
	links := map[string]string{}
	for i := range pictures {
		p := &pictures[i]
		for name, data := range p.Files() {
			files[name] = data
		}
		links[p.Name] = p.Name + ".d2"
		if p.SVG != nil {
			links[p.Name] = p.Name + ".svg"
		}
	}
	type row struct {
		Row
		Link string
	}
	rows := make([]row, len(v.Rows))
	for i := range v.Rows {
		rows[i] = row{Row: v.Rows[i], Link: links[v.Rows[i].Diagram]}
	}
	var page bytes.Buffer
	err := pageTemplate().Execute(&page, map[string]any{
		"Lang": v.text.Lang, "Title": fmt.Sprintf(v.text.AccessTitleFmt, v.Service),
		"T": v.text, "Columns": v.Columns, "Rows": rows, "Skipped": v.Skipped,
		"Light": d2.Light(), "Dark": d2.Dark(),
	})
	if err != nil {
		return nil, fmt.Errorf("policyview: render the page: %w", err)
	}
	files["index.html"] = page.Bytes()
	return files, nil
}

// pageTemplate is the access matrix page: the diagram-design skill's
// default skin (its palette, Geist with a system fallback, no shadows), a
// table whose header and first column stay in view while it scrolls.
func pageTemplate() *template.Template {
	return template.Must(template.New("page").Parse(pageHTML))
}

// pageHTML is page.html: markup, not text (its words come from locale).
//
//go:embed page.html
var pageHTML string
