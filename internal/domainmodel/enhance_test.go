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

// firstLine is the citation of the API types of the shop fixture.
const firstLine = "1-1"

// The written files of the shop fixture.
const (
	conceptsFile = domainmodel.ConceptsFile
	shopFile     = "shop.modelith.yaml"
)

// Names of the shop fixture's billing tables and payment types.
const (
	paymentType    = "Payment"
	refundableType = "Refundable"
	payGraphQL     = "pay.graphql"
	stateLine      = "3-3"
	invoicesTable  = "invoices"
	billingSQL     = "db/billing.sql"
	invoiceID      = "invoice_id"
)

// shopRule is a rule of domain, of EARS kind ears, touching entities.
func shopRule(id, domain, ears, requirement string, touches ...string) graph.Node {
	n := rule(id, requirement, touches...)
	n.Edges = append(n.Edges, graph.Edge{Type: graph.EdgeBelongsTo, To: "domain." + domain})
	n.Extra = map[string]string{"ears": ears}
	return n
}

// shopInput: orders and coupons match the human Order and Coupon; invoices
// is in no model, and owns its lines by a NOT NULL foreign key. Only
// single-entity ubiquitous rules become invariants. Of the API types,
// Customer is a concept no table stands for, Invoice is the invoices
// table, Store another service's and Legacy deprecated.
func shopInput(t *testing.T, set *domainmodel.Set) *domainmodel.EnhanceInput {
	t.Helper()
	return &domainmodel.EnhanceInput{
		Set: set,
		Nodes: []graph.Node{
			entity("entity.orders", "orders"),
			entity("entity.coupons", "coupons"),
			entity("entity.invoices", "invoices"),
			entity("entity.invoice_lines", "invoice_lines"),
			entity("entity.invoice_notes", "invoice_notes"),
			entity("entity.webapp.Receipt", "Receipt"),
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
		Tables:   shopTables(),
		APITypes: shopAPITypes(),
		Enums:    shopEnums(),
		Title:    "shop",
		Lang:     "en",
	}
}

// shopTables are the shop fixture's code tables.
func shopTables() []domainmodel.Table {
	return []domainmodel.Table{
		{
			ID:    "entity.orders",
			Title: "orders",
			Columns: []string{
				"id:int64",
				"total:numeric(10,2)",
				"placed_at:time.Time",
				"note:[]byte",
			},
			Source: extract.Ref{Path: "db/schema.sql", Lines: "1-7"},
		},
		{
			ID: "entity.coupons", Title: "coupons",
			Description: "A discount code a customer applies at checkout.",
			Source:      extract.Ref{Path: "db/schema.sql", Lines: "9-12"},
		},
		{
			ID: "entity.invoices", Title: invoicesTable,
			Columns: []string{"number:text"},
			Source:  extract.Ref{Path: billingSQL, Lines: "1-4"},
		},
		{
			ID: "entity.invoice_notes", Title: "invoice_notes",
			Columns:            []string{"invoice_id:bigint", "text:text"},
			ColumnDescriptions: map[string]string{"text": "What the clerk wrote."},
			Source:             extract.Ref{Path: billingSQL, Lines: "12-15"},
			PrimaryKey:         []string{invoiceID},
			References: []extract.Reference{
				{Columns: []string{invoiceID}, Table: invoicesTable, NotNull: true},
			},
		},
		{
			ID:       "entity.webapp.Receipt",
			Title:    "Receipt",
			StoredBy: "PaperReceipt",
			Columns: []string{
				"total:int64",
				"state:ReceiptState",
				"kind:*models.Kind",
				"old_state:other.ReceiptState",
			},
			Source: extract.Ref{Path: "receipts/receipt.go", Lines: "3-9"},
		},
		{
			ID: "entity.invoice_lines", Title: "invoice_lines",
			Columns: []string{"invoice_id:bigint", "coupon_id:bigint"},
			Source:  extract.Ref{Path: billingSQL, Lines: "6-10"},
			References: []extract.Reference{
				{Columns: []string{invoiceID}, Table: invoicesTable, NotNull: true},
				{Columns: []string{"coupon_id"}, Table: "coupons"},
			},
		},
	}
}

// shopAPITypes are the shop fixture's API types: owned, extended,
// deprecated, two interfaces and their implementations.
func shopAPITypes() []domainmodel.APIType {
	return []domainmodel.APIType{
		{
			Name:   "Customer",
			Owned:  true,
			Source: extract.Ref{Path: "customer.graphql", Lines: firstLine},
		},
		{
			Name:   "Invoice",
			Owned:  true,
			Source: extract.Ref{Path: "billing.graphql", Lines: firstLine},
		},
		{Name: "Store", Source: extract.Ref{Path: "store.graphql", Lines: firstLine}},
		{
			Name: paymentType, Owned: true, Interface: true,
			Description: "A way a customer pays.",
			Source:      extract.Ref{Path: payGraphQL, Lines: firstLine},
		},
		{
			Name: refundableType, Owned: true, Interface: true,
			Source: extract.Ref{Path: payGraphQL, Lines: "5-5"},
		},
		{
			Name: "CardPayment", Owned: true, Implements: []string{paymentType},
			Description: "A payment by card.",
			Source:      extract.Ref{Path: payGraphQL, Lines: "9-9"},
		},
		{
			Name: "GiftPayment", Owned: true, Implements: []string{paymentType, refundableType},
			Source: extract.Ref{Path: payGraphQL, Lines: "13-13"},
		},
		{
			Name:       "Legacy",
			Owned:      true,
			Deprecated: true,
			Source:     extract.Ref{Path: "old.graphql", Lines: firstLine},
		},
	}
}

// shopEnums are the shop fixture's enumerations: one in the Receipt
// table's directory, one of the same name elsewhere, one qualified.
func shopEnums() []domainmodel.Enum {
	return []domainmodel.Enum{
		{
			Name: "receipts.ReceiptState",
			Values: []domainmodel.EnumValue{
				{"ReceiptOpen", "OPEN"},
				{"ReceiptClosed", "CLOSED"},
			},
			Source: extract.Ref{Path: "receipts/state.go", Lines: stateLine},
		},
		{
			Name:   "other.ReceiptState", // same name, another directory: only when qualified
			Values: []domainmodel.EnumValue{{"Gone", "GONE"}},
			Source: extract.Ref{Path: "other/state.go", Lines: stateLine},
		},
		{
			Name:   "models.Kind",
			Values: []domainmodel.EnumValue{{"KindPaper", "0"}, {"KindEmail", "1"}},
			Source: extract.Ref{Path: "models/kind.go", Lines: stateLine},
		},
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
			wantFiles: []string{conceptsFile, shopFile},
		},
		"without a human model": {
			wantFiles: []string{conceptsFile},
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
			conceptsFile, "/entities/Invoice/definition", "placeholder",
		},
		"ownership from a NOT NULL foreign key": {
			conceptsFile, "/entities/Invoice/relationships/0", "references db/billing.sql:6-10",
		},
		"API type no table stands for": {
			conceptsFile, "/entities/Customer/definition", "placeholder",
		},
		"API type with a docstring": {
			conceptsFile, "/entities/CardPayment/definition", "description pay.graphql:9-9",
		},
		"one implemented interface": {
			conceptsFile, "/entities/CardPayment/subtypeOf", "implements pay.graphql:9-9",
		},
		"two implemented interfaces": {
			conceptsFile, "/entities/GiftPayment/subtypeOf", "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := out.Provenance[tc.file][tc.pointer]; got != tc.want {
				t.Errorf("provenance[%s][%s] = %q, want %q", tc.file, tc.pointer, got, tc.want)
			}
		})
	}
	want := map[string][]string{
		conceptsFile: {
			"Customer",
			"GiftPayment",
			"Invoice",
			"InvoiceLine",
			"InvoiceNote",
			"Receipt",
			"Refundable",
		},
	}
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
	set, err := domainmodel.Load(writeFiles(t, dir, first.Files))
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

// TestUnreviewedEntities: written models read back as --domain-model
// count each entity still defined by codetospec, placeholder or draft
// (the shop model's Coupon took a draft of its table's description), and
// a model people defined throughout counts none.
func TestUnreviewedEntities(t *testing.T) {
	out, err := domainmodel.Enhance(shopInput(t, loadShop(t)))
	if err != nil {
		t.Fatal(err)
	}
	set, err := domainmodel.Load(writeFiles(t, t.TempDir(), out.Files))
	if err != nil {
		t.Fatal(err)
	}
	report := domainmodel.Compare(set, shopInput(t, nil).Nodes, domainmodel.Options{})
	want := 0 // every definition but the ones people wrote
	for _, prov := range out.Provenance {
		for pointer, origin := range prov {
			if strings.HasSuffix(pointer, "/definition") && origin != "human" {
				want++
			}
		}
	}
	if got := report.UnreviewedEntities(); got != want || want == 0 {
		t.Errorf("unreviewed = %d, want %d (of %d)", got, want, len(report.Entities))
	}
	human := domainmodel.Compare(load(t), garageGraph(), domainmodel.Options{})
	if got := human.UnreviewedEntities(); got != 0 {
		t.Errorf("human model: unreviewed = %d, want 0", got)
	}
}

// TestEnhanceSingularNames: entity names are singular, and the next run
// matches each back to its table through its plurals.
func TestEnhanceSingularNames(t *testing.T) {
	titles := map[string]string{
		"policies": "Policy", "addresses": "Address", "assignment_students": "AssignmentStudent",
		"status": "Status", "class": "Class", "analysis": "Analysis",
		"student_data": "StudentData", "SubjectMasteryAssignment": "SubjectMasteryAssignment",
	}
	in := &domainmodel.EnhanceInput{Title: "names", Lang: "en"}
	for title := range titles {
		id := "entity." + strings.ToLower(title)
		in.Nodes = append(in.Nodes, entity(id, title))
		in.Tables = append(in.Tables, domainmodel.Table{
			ID: id, Title: title, Source: extract.Ref{Path: "schema.sql", Lines: firstLine},
		})
	}
	out, err := domainmodel.Enhance(in)
	if err != nil {
		t.Fatal(err)
	}
	set, err := domainmodel.Load(writeFiles(t, t.TempDir(), out.Files))
	if err != nil {
		t.Fatal(err)
	}
	report := domainmodel.Compare(set, in.Nodes, domainmodel.Options{})
	matched := map[string]string{} // entity name → table title
	for _, e := range report.Entities {
		_, name, _ := strings.Cut(e.Name, ".")
		matched[name] = e.Code.Title
	}
	for title, name := range titles {
		if matched[name] != title {
			t.Errorf("table %s: entity %s matches %q, want the table", title, name, matched[name])
		}
	}
}

// writeFiles writes files under dir and returns their paths.
func writeFiles(t *testing.T, dir string, files []domainmodel.File) []string {
	t.Helper()
	var paths []string
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
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
