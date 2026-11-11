package d2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RendererName is codetospec-d2's binary name, the one make build puts
// beside codetospec: --d2-renderer's default.
const RendererName = "codetospec-d2"

// MaxEdges is the most edges Draw sends to the renderer: ELK's time grows
// faster than the edges (measured on webapp's domain maps: 182 edges 16 s,
// 274 47 s, 378 two minutes, 1117 more than ten), and a run should not
// spend minutes on a picture nobody can read. A larger diagram keeps its
// source.
const MaxEdges = 300

// renderTimeout bounds one batch: ELK lays out each diagram in an embedded
// JavaScript engine, a few hundred milliseconds apiece.
const renderTimeout = 10 * time.Minute

// The protocol (codetospec-d2 mirrors these types): Batch{Diagrams:
// []Request} on stdin, Batch{Results: []Result} on stdout, in request
// order; a diagram D2 rejects has its Result.Error, the others their SVG.
type (
	// Request is one source to draw, named so its result can be told.
	Request struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	// Result is one drawn source.
	Result struct {
		Name  string `json:"name"`
		SVG   string `json:"svg,omitempty"`
		Error string `json:"error,omitempty"`
	}
	batch struct {
		Diagrams []Request `json:"diagrams,omitempty"`
		Results  []Result  `json:"results,omitempty"`
	}
)

// Renderer runs codetospec-d2 at Cmd. The zero Renderer draws nothing.
type Renderer struct {
	Cmd string
}

// FindRenderer is the renderer at cmd: a path as given, or a bare name
// looked up beside the running executable (make build puts both in bin/),
// then on PATH. The zero Renderer when none is found.
func FindRenderer(cmd string) Renderer {
	if strings.ContainsRune(cmd, filepath.Separator) {
		if _, err := os.Stat(cmd); err == nil {
			return Renderer{Cmd: cmd}
		}
		return Renderer{}
	}
	if self, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(self), cmd)
		if _, statErr := os.Stat(beside); statErr == nil {
			return Renderer{Cmd: beside}
		}
	}
	if path, err := exec.LookPath(cmd); err == nil {
		return Renderer{Cmd: path}
	}
	return Renderer{}
}

// Picture is one diagram as written: its D2 source in the Light palette
// and, when a renderer drew it, its SVG in Light and in Dark.
type Picture struct {
	Name, Source string
	SVG, DarkSVG []byte
}

// Files are p's files under its name: name.d2, and name.svg and
// name-dark.svg when drawn.
func (p *Picture) Files() map[string][]byte {
	files := map[string][]byte{p.Name + ".d2": []byte(p.Source)}
	if p.SVG != nil {
		files[p.Name+".svg"] = p.SVG
		files[p.Name+"-dark.svg"] = p.DarkSVG
	}
	return files
}

// Markdown shows p in a Markdown page whose diagrams are under dir: its
// SVG, the dark one under a dark theme, with alt as its alternative text,
// then a link to its source labelled sourceLabel; undrawn, its source in
// a d2 code block, which a reader can follow where nothing draws it.
func (p *Picture) Markdown(dir, alt, sourceLabel string) string {
	if p.SVG == nil {
		return "```d2\n" + p.Source + "```\n"
	}
	base := dir + "/" + p.Name
	return fmt.Sprintf("<picture>\n"+
		"  <source media=\"(prefers-color-scheme: dark)\" srcset=\"%s-dark.svg\">\n"+
		"  <img alt=\"%s\" src=\"%s.svg\">\n"+
		"</picture>\n\n[%s](%s.d2)\n", base, html.EscapeString(alt), base, sourceLabel, base)
}

// Draw is each named diagram as a Picture, in names' order, drawn by r in
// both palettes. The zero Renderer returns the sources alone; so does a
// failed batch, with its error, a diagram D2 rejects and one of more than
// MaxEdges edges, each error joined to the returned one: a picture is
// never worth failing a run.
func (r Renderer) Draw(
	ctx context.Context,
	names []string,
	ds map[string]*Diagram,
) ([]Picture, error) {
	light, dark := Light(), Dark()
	pictures := make([]Picture, len(names))
	var reqs []Request
	var drawn []int // the pictures sent, by index
	var errs []error
	for i, name := range names {
		pictures[i] = Picture{Name: name, Source: Source(ds[name], &light)}
		if n := len(ds[name].Edges); n > MaxEdges {
			errs = append(errs, fmt.Errorf("diagram %s: %d edges, more than %d: not drawn",
				name, n, MaxEdges))
			continue
		}
		drawn = append(drawn, i)
		reqs = append(reqs,
			Request{Name: name, Source: pictures[i].Source},
			Request{Name: name + "-dark", Source: Source(ds[name], &dark)})
	}
	if r.Cmd == "" {
		return pictures, nil // nothing draws: sources are what was asked for
	}
	if len(reqs) == 0 {
		return pictures, errors.Join(errs...)
	}
	results, err := r.render(ctx, reqs)
	if err != nil {
		return pictures, errors.Join(append(errs, err)...)
	}
	for k, i := range drawn {
		light, dark := results[2*k], results[2*k+1]
		if failed := firstError(light, dark); failed != "" {
			errs = append(errs, fmt.Errorf("diagram %s: %s", pictures[i].Name, failed))
			continue
		}
		pictures[i].SVG, pictures[i].DarkSVG = []byte(light.SVG), []byte(dark.SVG)
	}
	return pictures, errors.Join(errs...)
}

// firstError is the first of two results' errors, "" for none.
func firstError(a, b Result) string {
	if a.Error != "" {
		return a.Error
	}
	return b.Error
}

// render runs one batch through codetospec-d2.
func (r Renderer) render(ctx context.Context, reqs []Request) ([]Result, error) {
	in, err := json.Marshal(batch{Diagrams: reqs})
	if err != nil {
		return nil, fmt.Errorf("d2: encode the batch: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()
	// Running the renderer the person configured is the point.
	cmd := exec.CommandContext(runCtx, r.Cmd) //nolint:gosec // G204: see above.
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("d2: %s: %w: %s", r.Cmd, err, strings.TrimSpace(stderr.String()))
	}
	var out batch
	if err = json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("d2: %s: decode its output: %w", r.Cmd, err)
	}
	if len(out.Results) != len(reqs) {
		return nil, fmt.Errorf("d2: %s: %d results for %d diagrams", r.Cmd,
			len(out.Results), len(reqs))
	}
	for i := range reqs {
		if out.Results[i].Name != reqs[i].Name {
			return nil, fmt.Errorf("d2: %s: result %d is %q, want %q", r.Cmd, i,
				out.Results[i].Name, reqs[i].Name)
		}
	}
	return out.Results, nil
}
