package d2_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/d2"
)

// Node IDs the tests share.
const group, lib = "main", "lib"

// TestSource: nodes nest in their containers, edges name full paths, and
// a key or label keeps any character (D2 would read "." as nesting and
// "${" as a variable).
func TestSource(t *testing.T) {
	d := &d2.Diagram{
		Nodes: []d2.Node{
			{ID: group, Label: group, Class: d2.ClassGroup},
			{ID: "cmd.seed", Label: `seed "all" ${x}`, In: group},
			{ID: lib, Label: lib},
		},
		Edges: []d2.Edge{{From: "cmd.seed", To: lib, Label: "uses"}},
	}
	light, dark := d2.Light(), d2.Dark()
	got := d2.Source(d, &light)
	for _, want := range []string{
		"vars: {d2-config: {layout-engine: elk}}\n",
		"direction: down\n",
		`style: {fill: "#f5f5f5"}`,
		"\"main\": \"main\" {class: group\n  \"cmd.seed\": \"seed \\\"all\\\" \\${x}\" {class: step}\n}\n",
		"\"lib\": \"lib\" {class: step}\n",
		"\"main\".\"cmd.seed\" -> \"lib\": \"uses\"\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("source lacks %q:\n%s", want, got)
		}
	}
	if d2.Source(d, &light) != got {
		t.Error("Source is not deterministic")
	}
	if dark := d2.Source(d, &dark); !strings.Contains(dark, `style: {fill: "#2d3142"}`) {
		t.Errorf("dark source keeps the light paper:\n%s", dark)
	}
}

// TestPalettes: every colour is opaque "#rrggbb" (D2 rejects alpha), and
// the translucent roles are composited over the paper.
func TestPalettes(t *testing.T) {
	for name, p := range map[string]d2.Palette{"light": d2.Light(), "dark": d2.Dark()} {
		for _, c := range []string{
			p.Paper, p.Ink, p.Muted, p.Soft, p.Rule, p.Accent, p.AccentTint,
			p.Node, p.Group, p.Store, p.Input, p.External, p.ExternalStroke,
		} {
			if len(c) != len("#rrggbb") || c[0] != '#' {
				t.Errorf("%s: colour %q is not #rrggbb", name, c)
			}
		}
	}
	// ink #2d3142 at 0.12 over paper #f5f5f5.
	if d2.Light().Rule != "#dddde0" {
		t.Errorf("light rule = %s, want #dddde0", d2.Light().Rule)
	}
}

// TestDraw: without a renderer, pictures are their sources; with one,
// each gets its light and dark SVG, and a diagram the renderer rejects
// keeps its source while the others are drawn.
func TestDraw(t *testing.T) {
	ds := map[string]*d2.Diagram{
		"a": {Nodes: []d2.Node{{ID: "x", Label: "x"}}},
		"b": {Nodes: []d2.Node{{ID: "y", Label: "y"}}},
	}
	names := []string{"a", "b"}

	pics, err := d2.Renderer{}.Draw(context.Background(), names, ds)
	if err != nil || len(pics) != 2 || pics[0].SVG != nil || pics[0].Source == "" {
		t.Fatalf("zero renderer: %+v, %v", pics, err)
	}
	if files := pics[0].Files(); len(files) != 1 || files["a.d2"] == nil {
		t.Errorf("undrawn files = %v, want a.d2 alone", files)
	}

	fake := filepath.Join(t.TempDir(), d2.RendererName)
	script := `#!/bin/sh
cat >/dev/null
echo '{"results":[{"name":"a","svg":"<svg>a</svg>"},{"name":"a-dark","svg":"<svg>A</svg>"},` +
		`{"name":"b","error":"bad"},{"name":"b-dark","svg":"<svg>B</svg>"}]}'
`
	err = os.WriteFile(fake, []byte(script), 0o700) //nolint:gosec // G306: the fake must run.
	if err != nil {
		t.Fatal(err)
	}
	r := d2.FindRenderer(fake)
	pics, err = r.Draw(context.Background(), names, ds)
	if err == nil || !strings.Contains(err.Error(), "diagram b: bad") {
		t.Errorf("err = %v, want diagram b's", err)
	}
	if string(pics[0].SVG) != "<svg>a</svg>" || string(pics[0].DarkSVG) != "<svg>A</svg>" {
		t.Errorf("a = %q / %q", pics[0].SVG, pics[0].DarkSVG)
	}
	if pics[1].SVG != nil || pics[1].Source == "" {
		t.Errorf("b = %+v, want its source alone", pics[1])
	}
	if files := pics[0].Files(); len(files) != 3 || files["a-dark.svg"] == nil {
		t.Errorf("drawn files = %v", files)
	}
}

// TestDrawSkipsLargeDiagrams: a diagram of more than MaxEdges edges is
// never sent; its picture is its source, and the error says why.
func TestDrawSkipsLargeDiagrams(t *testing.T) {
	big := &d2.Diagram{}
	for i := range d2.MaxEdges + 1 {
		big.Edges = append(big.Edges, d2.Edge{From: "a", To: strconv.Itoa(i)})
	}
	fake := filepath.Join(t.TempDir(), d2.RendererName)
	// The fake fails if called: nothing may be sent.
	failing := []byte("#!/bin/sh\nexit 1\n")
	err := os.WriteFile(fake, failing, 0o700) //nolint:gosec // G306: the fake must run.
	if err != nil {
		t.Fatal(err)
	}
	pics, err := d2.FindRenderer(fake).Draw(t.Context(), []string{"big"},
		map[string]*d2.Diagram{"big": big})
	if err == nil || !strings.Contains(err.Error(), "not drawn") {
		t.Errorf("err = %v, want the size", err)
	}
	if pics[0].SVG != nil || pics[0].Source == "" {
		t.Errorf("picture = %+v, want its source alone", pics[0])
	}
}

// TestFindRenderer: a path that doesn't exist, or a name found nowhere,
// is no renderer.
func TestFindRenderer(t *testing.T) {
	for _, cmd := range []string{
		filepath.Join(t.TempDir(), "absent"), "codetospec-d2-absent-from-path",
	} {
		if r := d2.FindRenderer(cmd); r.Cmd != "" {
			t.Errorf("FindRenderer(%q) = %q, want none", cmd, r.Cmd)
		}
	}
}

// TestRendererTestdata: codetospec-d2's test sources (renderers/d2, a
// module of its own) are this package's output, so the renderer is tested
// on what codetospec sends. UPDATE_GOLDEN=1 rewrites them.
func TestRendererTestdata(t *testing.T) {
	d := &d2.Diagram{
		Nodes: []d2.Node{
			{ID: group, Label: group, Class: d2.ClassGroup},
			{ID: "cmd-a", Label: "cmd-a", In: group},
			{ID: "cmd.b", Label: "cmd-b", In: group},
			{ID: lib, Label: lib, Class: d2.ClassFocal},
			{ID: "q", Label: "Signed in?", Class: d2.ClassDecision},
			{ID: "end", Label: "Allowed", Class: d2.ClassTerminal},
		},
		Edges: []d2.Edge{
			{From: group, To: lib},
			{From: "cmd.b", To: "q", Label: "uses"},
			{From: "q", To: "end", Label: "yes"},
		},
	}
	light, dark := d2.Light(), d2.Dark()
	for path, p := range map[string]*d2.Palette{
		"../../renderers/d2/testdata/light.d2": &light,
		"../../renderers/d2/testdata/dark.d2":  &dark,
	} {
		want := d2.Source(d, p)
		if os.Getenv("UPDATE_GOLDEN") != "" {
			if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path) //nolint:gosec // G304: the paths are this test's literals.
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s is stale: UPDATE_GOLDEN=1 go test ./internal/d2", path)
		}
	}
}
