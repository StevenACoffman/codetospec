package packagemap_test

import (
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/d2"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/packagemap"
)

const (
	module = "github.com/Khan/districts-jobs/roster"
	shared = "github.com/Khan/districts-jobs/pkg"
	none   = "No dependencies"
)

func pkgFact(name, path, root string) extract.Fact {
	return extract.Fact{
		Kind: extract.KindPackage, ID: "package." + name,
		Attrs:  map[string]string{extract.AttrName: name},
		Source: extract.Ref{Path: path, Lines: "1-1", Root: root},
	}
}

func importFact(path, namespace, target string) extract.Fact {
	return extract.Fact{
		Kind: extract.KindImport, ID: "import." + path + "#" + target,
		Attrs:  map[string]string{extract.AttrTarget: target, extract.AttrNamespace: namespace},
		Source: extract.Ref{Path: path, Lines: "3-3"},
	}
}

// edges is d's edges as "from -> to" labels.
func edges(d *d2.Diagram) []string {
	label := map[string]string{}
	for _, n := range d.Nodes {
		label[n.ID] = n.Label
	}
	var out []string
	for _, e := range d.Edges {
		out = append(out, label[e.From]+" -> "+label[e.To])
	}
	return out
}

// TestBuildFromPackageFacts: imports resolve by exact import path,
// whatever the package is called and in either tree; a shared package is
// external and drawn only when imported; labels drop the shared prefix;
// third-party imports are not packages of the code.
func TestBuildFromPackageFacts(t *testing.T) {
	facts := []extract.Fact{
		pkgFact(module+"/cmd", "cmd/command.go", ""),
		pkgFact(module+"/cmd/pull-topic", "cmd/pull-topic/topic.go", ""),
		pkgFact(module+"/pkg/rostering", "pkg/rostering/sync.go", ""),
		pkgFact(module+"/pkg/unused", "pkg/unused/x.go", ""),
		pkgFact(shared+"/errors", "errors/khan.go", "pkg"),
		pkgFact(shared+"/ptr", "ptr/ptr.go", "pkg"),
		importFact("cmd/command.go", "cmd", module+"/cmd/pull-topic"),
		importFact("cmd/pull-topic/topic.go", "pull_topic", module+"/pkg/rostering"),
		importFact("cmd/pull-topic/topic.go", "pull_topic", shared+"/errors"),
		importFact("pkg/rostering/sync.go", "rostering", shared+"/errors"),
		importFact("pkg/rostering/sync.go", "rostering", "go.uber.org/zap"),
	}
	d := packagemap.Build(facts, none)
	want := []string{
		"roster/cmd -> roster/cmd/pull-topic",
		"roster/cmd/pull-topic -> pkg/errors",
		"roster/cmd/pull-topic -> roster/pkg/rostering",
		"roster/pkg/rostering -> pkg/errors",
	}
	if got := edges(d); !slices.Equal(got, want) {
		t.Errorf("edges = %q\nwant    %q", got, want)
	}
	classes := map[string]string{}
	for _, n := range d.Nodes {
		classes[n.Label] = n.Class
	}
	if classes["pkg/errors"] != d2.ClassExternal || classes["roster/cmd"] != d2.ClassStep {
		t.Errorf("classes = %v, want shared packages external", classes)
	}
	if _, drawn := classes["pkg/ptr"]; drawn {
		t.Error("a shared package nothing imports is drawn")
	}
	if _, drawn := classes["roster/pkg/unused"]; !drawn {
		t.Error("the code's own unconnected package is missing")
	}
}

// TestBuildFromModuleFacts: without package facts (PHP), a package is a
// namespace, an import's file belongs to the namespace it declares, and
// a class import resolves to the longest namespace prefixing it.
func TestBuildFromModuleFacts(t *testing.T) {
	module := func(name, path string) extract.Fact {
		return extract.Fact{
			Kind: extract.KindModule, ID: "module." + name,
			Attrs:  map[string]string{extract.AttrName: name},
			Source: extract.Ref{Path: path, Lines: "1-1"},
		}
	}
	facts := []extract.Fact{
		module(`App\Http\Controllers`, "app/Http/Controllers/Billing.php"),
		module(`App\Services`, "app/Services/Plan.php"),
		module(`App\Services\Billing`, "app/Services/Billing/Calc.php"),
		importFact("app/Http/Controllers/Billing.php", `App\Http\Controllers`,
			`App\Services\Billing\ProrataCalculator`),
	}
	got := edges(packagemap.Build(facts, none))
	if want := []string{`Http\Controllers -> Services\Billing`}; !slices.Equal(got, want) {
		t.Errorf("edges = %q, want %q", got, want)
	}
}
