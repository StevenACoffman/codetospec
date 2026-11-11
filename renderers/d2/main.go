// Command codetospec-d2 draws D2 sources as SVG for codetospec: a batch
// of {"diagrams": [{"name", "source"}]} on stdin, {"results": [{"name",
// "svg" | "error"}]} on stdout, in request order (the protocol of
// codetospec's internal/d2, which this mirrors). A source D2 rejects gets
// its error and the others are still drawn; anything else (bad input, a
// font that doesn't load) fails the batch with a nonzero exit.
//
// It lives in a module of its own so the D2 library's dependencies (an
// embedded JavaScript engine for ELK) stay out of codetospec's. Every
// diagram is laid out by ELK, whose right-angle connectors the
// diagram-design skill requires, whatever layout engine a source names,
// and set in Geist (fonts/, SIL Open Font License, the skill's typeface).
// The output omits D2's version, so a rebuild against the same layout
// gives the same bytes.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2fonts"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"
)

// The protocol, as internal/d2 declares it.
type (
	request struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	result struct {
		Name  string `json:"name"`
		SVG   string `json:"svg,omitempty"`
		Error string `json:"error,omitempty"`
	}
	batch struct {
		Diagrams []request `json:"diagrams,omitempty"`
		Results  []result  `json:"results,omitempty"`
	}
)

// The Geist styles D2 sets text in: labels, container labels, bold.
var (
	//go:embed fonts/Geist-Regular.ttf
	geistRegular []byte
	//go:embed fonts/Geist-SemiBold.ttf
	geistSemiBold []byte
	//go:embed fonts/Geist-Bold.ttf
	geistBold []byte
)

// pad is the margin around each drawing, in pixels.
const pad = 16

func main() {
	os.Exit(run(context.Background(), os.Stdin, os.Stdout, os.Stderr))
}

// run draws the batch on stdin to stdout, its exit status.
func run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	var in batch
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		logger.ErrorContext(ctx, "cannot read the batch", "err", err)
		return 1
	}
	font, err := d2fonts.AddFontFamily("Geist", geistRegular, nil, geistBold, geistSemiBold)
	if err != nil {
		logger.ErrorContext(ctx, "cannot load the fonts", "err", err)
		return 1
	}
	out := batch{Results: drawAll(log.With(ctx, logger), in.Diagrams, font)}
	if err = json.NewEncoder(stdout).Encode(out); err != nil {
		logger.ErrorContext(ctx, "cannot write the results", "err", err)
		return 1
	}
	return 0
}

// drawAll draws each request, as many at once as there are CPUs: ELK
// takes a few hundred milliseconds a diagram. Results in request order.
func drawAll(ctx context.Context, reqs []request, font *d2fonts.FontFamily) []result {
	results := make([]result, len(reqs))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(reqs)) {
		wg.Go(func() {
			for i := range next {
				results[i] = result{Name: reqs[i].Name}
				svg, err := draw(ctx, reqs[i].Source, font)
				if err != nil {
					results[i].Error = err.Error()
					continue
				}
				results[i].SVG = string(svg)
			}
		})
	}
	for i := range reqs {
		next <- i
	}
	close(next)
	wg.Wait()
	return results
}

// draw compiles one source, lays it out with ELK and renders it as SVG.
func draw(ctx context.Context, source string, font *d2fonts.FontFamily) ([]byte, error) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, fmt.Errorf("text ruler: %w", err)
	}
	padding, omitVersion := int64(pad), true
	opts := &d2svg.RenderOpts{Pad: &padding, OmitVersion: &omitVersion}
	diagram, _, err := d2lib.Compile(ctx, source, &d2lib.CompileOptions{
		Ruler:      ruler,
		FontFamily: font,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2elklayout.DefaultLayout, nil
		},
	}, opts)
	if err != nil {
		return nil, err
	}
	return d2svg.Render(diagram, opts)
}
