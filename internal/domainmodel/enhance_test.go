package domainmodel_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/domainmodel"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// The written files of the shop fixture.
const (
	billingFile = "generated/billing.modelith.yaml"
	shopFile    = "shop.modelith.yaml"
)

// shopRule is a rule of domain, of EARS kind ears, touching entities.
func shopRule(id, domain, ears, requirement string, touches ...string) graph.Node {
	n := rule(id, requirement, touches...)
	n.Edges = append(n.Edges, graph.Edge{Type: graph.EdgeBelongsTo, To: "domain." + domain})
	n.Extra = map[string]string{"ears": ears}
	return n
}

// shopInput: orders and coupons match the human Order and Coupon; invoices
// is in no model. Only single-entity ubiquitous rules become invariants.
func shopInput(t *testing.T, set *domainmodel.Set) *domainmodel.EnhanceInput {
	t.Helper()
	return &domainmodel.EnhanceInput{
		Set: set,
		Nodes: []graph.Node{
			entity("entity.orders", "orders"),
			entity("entity.coupons", "coupons"),
			entity("entity.invoices", "invoices"),
			shopRule("rule.shop.order-total", "shop", "ubiquitous",
				"The system shall keep an order `total` equal to the sum of its lines.",
				"entity.orders"),
			shopRule("rule.shop.order-audit", "shop", "event",
				"WHEN an order is placed, the system shall log it.", "entity.orders"),
			shopRule("rule.shop.coupon-order", "shop", "ubiquitous",
				"The system shall apply one coupon per order.", "entity.orders", "entity.coupons"),
			shopRule("rule.billing.invoice-number", "billing", "ubiquitous",
				"The system shall number invoices without gaps.", "entity.invoices"),
		},
		Tables: []domainmodel.Table{
			{
				ID:     "entity.orders",
				Title:  "orders",
				Domain: "shop",
				Columns: []string{
					"id:int64",
					"total:numeric(10,2)",
					"placed_at:time.Time",
					"note:[]byte",
				},
				Source: extract.Ref{Path: "db/schema.sql", Lines: "1-7"},
			},
			{
				ID: "entity.coupons", Title: "coupons", Domain: "shop",
				Description: "A discount code a customer applies at checkout.",
				Source:      extract.Ref{Path: "db/schema.sql", Lines: "9-12"},
			},
			{
				ID: "entity.invoices", Title: "invoices", Domain: "other",
				Columns: []string{"number:text"},
				Source:  extract.Ref{Path: "db/billing.sql", Lines: "1-4"},
			},
		},
		Lang: "en",
	}
}

func loadShop(t *testing.T) *domainmodel.Set {
	t.Helper()
	set, err := domainmodel.Load([]string{"testdata/shop.modelith.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// TestEnhanceGolden pins the written files, with and without a human
// model. Regenerate with UPDATE_GOLDEN=1 go test ./internal/domainmodel,
// and review.
func TestEnhanceGolden(t *testing.T) {
	cases := map[string]struct {
		set       *domainmodel.Set
		wantFiles []string
	}{
		"with the human model": {
			set:       loadShop(t),
			wantFiles: []string{billingFile, shopFile},
		},
		"without a human model": {
			wantFiles: []string{billingFile, "generated/shop.modelith.yaml"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := domainmodel.Enhance(shopInput(t, tc.set))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range out.Files {
				got = append(got, f.Path)
				checkGolden(t, filepath.Join(
					"testdata",
					"enhance",
					strings.ReplaceAll(
						name,
						" ",
						"-",
					)+"."+strings.ReplaceAll(
						f.Path,
						"/",
						"_",
					),
				), f.Data)
			}
			if !slices.Equal(got, tc.wantFiles) {
				t.Errorf("files = %v, want %v", got, tc.wantFiles)
			}
		})
	}
}

// checkGolden compares data with the golden file, or rewrites it when
// UPDATE_GOLDEN=1.
func checkGolden(t *testing.T, golden string, data []byte) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.Clean(golden))
	if err != nil {
		t.Fatalf("missing golden (run with UPDATE_GOLDEN=1): %v", err)
	}
	if !bytes.Equal(data, want) {
		t.Errorf("output differs from %s:\n%s", golden, data)
	}
}

// TestEnhanceProvenance: every field codetospec decided names its origin,
// and placeholders are counted as undefined.
func TestEnhanceProvenance(t *testing.T) {
	out, err := domainmodel.Enhance(shopInput(t, loadShop(t)))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		file, pointer, want string
	}{
		"human definition kept": {
			shopFile, "/entities/Order/definition", "human",
		},
		"placeholder replaced by the description": {
			shopFile, "/entities/Coupon/definition", "description db/schema.sql:9-12",
		},
		"missing column added": {
			shopFile, "/entities/Order/attributes/1", "column db/schema.sql:1-7",
		},
		"unstated ubiquitous rule added": {
			shopFile, "/entities/Order/invariants/0", "rule rule.shop.order-total",
		},
		"generated entity without a description": {
			billingFile, "/entities/Invoices/definition", "placeholder",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := out.Provenance[tc.file][tc.pointer]; got != tc.want {
				t.Errorf("provenance[%s][%s] = %q, want %q", tc.file, tc.pointer, got, tc.want)
			}
		})
	}
	want := map[string][]string{billingFile: {"Invoices"}}
	if !reflect.DeepEqual(out.Undefined, want) {
		t.Errorf("undefined = %v, want %v", out.Undefined, want)
	}
}

// TestEnhanceRoundTrip: a written model, read back as a human model, comes
// out unchanged: nothing is added twice, and the definitions stay.
func TestEnhanceRoundTrip(t *testing.T) {
	first, err := domainmodel.Enhance(shopInput(t, loadShop(t)))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var paths []string
	for _, f := range first.Files {
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o750); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}
		if writeErr := os.WriteFile(path, f.Data, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		paths = append(paths, path)
	}
	set, err := domainmodel.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	in := shopInput(t, set)
	in.MirrorPath = func(p string) string {
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			t.Fatal(relErr)
		}
		return filepath.ToSlash(rel)
	}
	second, err := domainmodel.Enhance(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.Files, first.Files) {
		for i := range second.Files {
			t.Errorf("%s:\n%s", second.Files[i].Path, second.Files[i].Data)
		}
		t.Fatal("a second pass changed the files")
	}
}

// TestEnhanceIgnoresNodeOrder: reversed nodes and tables give the same
// files.
func TestEnhanceIgnoresNodeOrder(t *testing.T) {
	want, err := domainmodel.Enhance(shopInput(t, loadShop(t)))
	if err != nil {
		t.Fatal(err)
	}
	in := shopInput(t, loadShop(t))
	slices.Reverse(in.Nodes)
	slices.Reverse(in.Tables)
	got, err := domainmodel.Enhance(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("reversed input changed the result")
	}
}
