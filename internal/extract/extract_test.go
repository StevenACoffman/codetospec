package extract_test

import (
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
)

// Domains and strategies the cases share.
const (
	billingDomain = "billing"
	coreDomain    = "core"
	autoStrategy  = "auto"
	modelsDomain  = "models"
)

func fact(id, origin, certainty string) extract.Fact {
	return extract.Fact{Kind: "route", ID: id, Origin: origin, Certainty: certainty}
}

func TestMergePriority(t *testing.T) {
	sitterFacts := []extract.Fact{
		fact("route.get./a", extract.OriginSitter, extract.CertaintyStatic),
	}
	extractorFacts := []extract.Fact{fact("route.get./a", "php", extract.CertaintyStatic)}
	provedFacts := []extract.Fact{fact("route.get./a", "php", extract.CertaintyProved)}

	merged := extract.Merge(sitterFacts, extractorFacts)
	if len(merged) != 1 || merged[0].Origin != "php" {
		t.Fatalf("extractor should beat sitter, got %+v", merged)
	}

	merged = extract.Merge(provedFacts, extractorFacts)
	if len(merged) != 1 || merged[0].Certainty != extract.CertaintyProved {
		t.Fatalf("proved should beat static regardless of order, got %+v", merged)
	}

	merged = extract.Merge(extractorFacts, provedFacts)
	if len(merged) != 1 || merged[0].Certainty != extract.CertaintyProved {
		t.Fatalf("proved should override an earlier static fact, got %+v", merged)
	}
}

func TestMergeSortsAndKeepsDistinctIDs(t *testing.T) {
	merged := extract.Merge(
		[]extract.Fact{fact("b", extract.OriginSitter, extract.CertaintyStatic)},
		[]extract.Fact{fact("a", extract.OriginSitter, extract.CertaintyStatic)},
	)
	if len(merged) != 2 || merged[0].ID != "a" || merged[1].ID != "b" {
		t.Fatalf("merge should sort by id, got %+v", merged)
	}
}

func TestDomainOf(t *testing.T) {
	cases := []struct {
		namespace string
		path      string
		want      string
	}{
		{`App\Services\Billing`, "app/Services/Billing/X.php", "services"},
		{`App\Models`, "app/Models/Invoice.php", modelsDomain},
		{billingDomain, "internal/billing/calc.go", billingDomain},
		{"", "routes/web.php", "routes"},
		{"", "web.php", "root"},
		{"crate::billing::rules", "src/billing/rules.rs", billingDomain},
	}
	for _, c := range cases {
		f := extract.Fact{Attrs: map[string]string{}}
		if c.namespace != "" {
			f.Attrs["namespace"] = c.namespace
		}
		if got := extract.DomainOf(&f, c.path); got != c.want {
			t.Errorf("DomainOf(ns=%q, path=%q) = %q, want %q", c.namespace, c.path, got, c.want)
		}
	}
}

func TestDomainDepthSplitsSingleRoot(t *testing.T) {
	ns := `AP\Core\Controller\Sdk`
	cases := map[int]string{
		1: coreDomain,
		2: "core-controller",
		3: "core-controller-sdk",
		9: "core-controller-sdk", // capped at available segments
	}
	for depth, want := range cases {
		got := extract.DomainResolver{Strategy: autoStrategy, Depth: depth}.Resolve(ns, "src/x.php")
		if got != want {
			t.Errorf("depth %d = %q, want %q", depth, got, want)
		}
	}
	// depth 0 behaves like depth 1 (backward compatible).
	if got := (extract.DomainResolver{Strategy: autoStrategy}).Resolve(
		ns,
		"src/x.php",
	); got != "core" {
		t.Errorf("depth 0 = %q, want core", got)
	}
	// short namespace: depth caps gracefully.
	if got := (extract.DomainResolver{Strategy: autoStrategy, Depth: 3}).Resolve(
		`AP\Core`,
		"x.php",
	); got != "core" {
		t.Errorf("short ns = %q, want core", got)
	}
}

func TestDomainResolverStrategies(t *testing.T) {
	directory := extract.DomainResolver{Strategy: "directory"}
	if got := directory.Resolve(`App\Services\Billing`, "app/Services/X.php"); got != "app" {
		t.Errorf("directory strategy = %q, want app", got)
	}
	namespace := extract.DomainResolver{Strategy: "namespace"}
	if got := namespace.Resolve("", "app/Services/X.php"); got != "app" {
		t.Errorf("namespace strategy without namespace should fall back to path, got %q", got)
	}
}

func TestParseLines(t *testing.T) {
	a, b, err := extract.ParseLines("12-87")
	if err != nil || a != 12 || b != 87 {
		t.Fatalf("ParseLines(12-87) = %d,%d,%v", a, b, err)
	}
	for _, invalid := range []string{"", "12", "12-", "0-5", "9-3", "a-b", "1-2x"} {
		if _, _, parseErr := extract.ParseLines(invalid); parseErr == nil {
			t.Errorf("ParseLines(%q) should fail", invalid)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"POST /api/activate":  "post-api-activate",
		"App Services":        "app-services",
		"éé--weird__ chars!!": "weird-chars",
	}
	for in, want := range cases {
		if got := extract.Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
