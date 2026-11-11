package communities_test

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/communities"
)

// tools is two Go command tools, each with its own library: cmd/one uses
// lib/one, cmd/two uses lib/two. Both tools share the domain "main", the way
// a namespace strategy assigns every Go main package.
// The domains the tests assign.
const (
	mainDomain  = "main"
	mixedDomain = "mixed"
)

func tools() ([]communities.File, []communities.Import) {
	var files []communities.File
	var imports []communities.Import
	for _, tool := range []string{"one", "two"} {
		for _, name := range []string{"a.go", "b.go", "c.go"} {
			cmd := "cmd/" + tool + "/" + name
			files = append(files,
				communities.File{Path: cmd, Namespace: mainDomain, Domains: []string{mainDomain}},
				communities.File{
					Path: "lib/" + tool + "/" + name, Namespace: tool,
					Domains: []string{"lib-" + tool},
				},
			)
			imports = append(
				imports,
				communities.Import{From: cmd, Target: "example.com/svc/lib/" + tool},
			)
		}
	}
	return files, imports
}

func TestSplits(t *testing.T) {
	files, imports := tools()
	cases := map[string]struct {
		files   []communities.File
		imports []communities.Import
		want    []communities.Split
	}{
		"one domain over two unrelated tools is split": {
			files, imports,
			[]communities.Split{{Domain: mainDomain, Communities: 2, Files: 6}},
		},
		"one file per community is not a split": {
			files[:2], nil, nil,
		},
		"a lone file elsewhere is not a split": {
			append(files[:len(files):len(files)], communities.File{
				Path: "other/x.go", Namespace: "other", Domains: []string{"lib-one"},
			}),
			imports,
			[]communities.Split{{Domain: mainDomain, Communities: 2, Files: 6}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := communities.Splits(tc.files, communities.Of(tc.files, tc.imports))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Splits = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestSplitsResolvesImports: a target names files by namespace, by a member
// of a namespace, or by the directory a path ends with.
func TestSplitsResolvesImports(t *testing.T) {
	cases := map[string]string{
		"namespace":           `App\Billing`,
		"member of namespace": `App\Billing\Invoice`,
		"directory suffix":    "example.com/svc/billing",
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			// app/ and billing/ form one community only if the import links
			// them; the domain "mixed" spans both directories.
			var files []communities.File
			var imports []communities.Import
			for _, n := range []string{"a", "b", "c"} {
				files = append(
					files,
					communities.File{
						Path:      "app/" + n,
						Namespace: "App",
						Domains:   []string{mixedDomain},
					},
					communities.File{
						Path:      "billing/" + n,
						Namespace: `App\Billing`,
						Domains:   []string{mixedDomain},
					},
					communities.File{
						Path:      "audit/" + n,
						Namespace: `App\Audit`,
						Domains:   []string{mixedDomain},
					},
				)
				imports = append(imports, communities.Import{From: "app/" + n, Target: target})
			}
			got := communities.Splits(files, communities.Of(files, imports))
			want := []communities.Split{{Domain: mixedDomain, Communities: 2, Files: 9}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf(
					"Splits = %+v, want %+v (app and billing should be one community)",
					got,
					want,
				)
			}
		})
	}
}

// TestSplitsDeterministic: repeated runs, and inputs in another order, give
// the same splits.
func TestSplitsDeterministic(t *testing.T) {
	files, imports := tools()
	want := communities.Splits(files, communities.Of(files, imports))
	cases := map[string]struct {
		files   []communities.File
		imports []communities.Import
	}{
		"same input": {files, imports},
		"reversed files and imports": {
			reversed(files), reversed(imports),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for range 20 {
				if got := communities.Splits(
					tc.files,
					communities.Of(tc.files, tc.imports),
				); !reflect.DeepEqual(
					got,
					want,
				) {
					t.Fatalf("run differs: %+v vs %+v", got, want)
				}
			}
		})
	}
}

// TestOfDeterministic: a graph with many ties (240 files in 40
// directories, 900 scattered imports) gets the same communities on every
// call. gonum walked a simple graph's neighbours in map order, and calls
// differed on such graphs.
func TestOfDeterministic(t *testing.T) {
	const dirs, perDir, imports = 40, 6, 900
	var files []communities.File
	for d := range dirs {
		for f := range perDir {
			files = append(files, communities.File{Path: fmt.Sprintf("d%02d/f%d.go", d, f)})
		}
	}
	var edges []communities.Import
	// A linear congruential sequence (glibc's constants) scatters them,
	// the same on every run.
	x := uint32(1)
	next := func(n int) int {
		x = x*1103515245 + 12345
		return int(x>>16) % n
	}
	for range imports {
		from := files[next(len(files))].Path
		edges = append(edges, communities.Import{
			From: from, Target: fmt.Sprintf("example.com/x/d%02d", next(dirs)),
		})
	}
	want := communities.Of(files, edges)
	for i := range 40 {
		if got := communities.Of(files, edges); !reflect.DeepEqual(got, want) {
			t.Fatalf("call %d gives other communities", i)
		}
	}
}

// reversed returns a reversed copy of s.
func reversed[T any](s []T) []T {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}
