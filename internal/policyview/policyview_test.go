package policyview_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/cedarpolicy"
	"github.com/StevenACoffman/codetospec/internal/d2"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/policy"
	"github.com/StevenACoffman/codetospec/internal/policyview"
)

const (
	service   = "billing"
	viewCap   = "can_view_invoices"
	viewGo    = "CanViewInvoices"
	goldenDir = "testdata/"
	// subjectFrom is where the user concerned comes from in the request.
	subjectFrom = "parent"
)

// check is a one-check expression.
func check(l *policy.Leaf) policy.Expr {
	return policy.Expr{Op: policy.OpCheck, Check: l}
}

// billingModel: Invoice.total denies unless the caller is the user
// concerned or holds can_view_invoices on any scope; Invoice.refund also
// needs a signed-in caller and is rate-limited; Invoice.pdf is left out.
func billingModel() policy.Model {
	self := policy.Leaf{
		Kind:        policy.LeafSelf,
		Var:         "self",
		Subject:     "user",
		SubjectFrom: subjectFrom,
	}
	capability := policy.Leaf{
		Kind: policy.LeafCapability, Var: "cap", Capability: viewCap,
		Scope: policy.ScopeAny,
	}
	login := policy.Leaf{Kind: policy.LeafLogin, Var: "login"}
	ownerOrViewer := policy.Deny{RuleID: "rule.billing.total", When: policy.Expr{
		Op: policy.OpNot, Args: []policy.Expr{{Op: policy.OpAny, Args: []policy.Expr{
			check(&self), check(&capability),
		}}},
	}}
	signedIn := policy.Deny{RuleID: "rule.billing.login", When: policy.Expr{
		Op: policy.OpNot, Args: []policy.Expr{check(&login)},
	}}
	return policy.Model{
		Operations: []policy.Operation{
			{
				Action: "Invoice.refund", Point: "Invoice.refund", Subject: subjectFrom,
				Denies: []policy.Deny{signedIn, ownerOrViewer},
				Obligations: []policy.Obligation{{
					RuleID: "rule.billing.limit", Check: "ratelimit.ByUser",
					RateLimit: policy.RateLimit{Key: "refund", Count: "5", Window: "time.Hour"},
				}},
			},
			{
				Action: "Invoice.total", Point: "Invoice.total", Subject: subjectFrom,
				Denies: []policy.Deny{ownerOrViewer},
			},
		},
		Capabilities: []policy.Capability{{Name: viewCap, GoName: viewGo}},
		Roles: []policy.Role{
			{Name: "Accountant", Capabilities: []string{viewCap}},
			{Name: "Clerk", Capabilities: []string{viewCap, "can_file"}},
		},
	}
}

func build(t *testing.T) *policyview.Views {
	t.Helper()
	m := billingModel()
	spec, err := cedarpolicy.Export(&m, service)
	if err != nil {
		t.Fatal(err)
	}
	decider, err := cedarpolicy.NewDecider(&spec, service)
	if err != nil {
		t.Fatal(err)
	}
	return policyview.Build(&policyview.Input{
		Service: service, Model: &m, Decider: decider,
		Skipped: map[string][]string{"Invoice.pdf": {"guard names no check"}},
		Text:    locale.For("en"),
	})
}

// TestMatrix: callers decided alike share a column, in the order of their
// first persona, and each cell says what the exported policies decide:
// no principal but a user passes a self check, and every grant puts its
// holder in the any-scope group, so each role at each scope, the site
// administrator and the explicit holder decide alike.
func TestMatrix(t *testing.T) {
	v := build(t)
	const self = "depends on: the caller is the user concerned"
	want := []struct {
		label string
		cells []string
	}{
		{
			"Anonymous caller · Push message from the queue · Service administrator call",
			[]string{"denied", "denied"},
		},
		{"Signed-in user, no grant", []string{self, self}},
		{"Site administrator · Accountant (global, all users, NWEA MAP users, one country, " +
			"one locale, content) · Clerk (global, all users, NWEA MAP users, one country, " +
			"one locale, content) · holds “View invoices” itself", []string{"allowed", "allowed"}},
	}
	if len(v.Columns) != len(want) {
		t.Fatalf("%d columns, want %d: %+v", len(v.Columns), len(want), v.Columns)
	}
	for i, w := range want {
		got := []string{v.Rows[0].Cells[i].Text, v.Rows[1].Cells[i].Text}
		if v.Columns[i].Label != w.label || !slices.Equal(got, w.cells) {
			t.Errorf(
				"column %d = %q %q\nwant         %q %q",
				i,
				v.Columns[i].Label,
				got,
				w.label,
				w.cells,
			)
		}
	}
	if v.Personas != 4+1+2*len(policy.GrantScopes())+1 {
		t.Errorf("%d personas, want the fixed four, the site administrator, two roles at "+
			"every scope and one explicit holder", v.Personas)
	}
	if v.Rows[0].Limited == "" || v.Rows[1].Limited != "" {
		t.Errorf("rate limits = %q / %q, want refund's alone", v.Rows[0].Limited, v.Rows[1].Limited)
	}
	if len(v.Skipped) != 1 || v.Skipped[0].Operation != "Invoice.pdf" {
		t.Errorf("skipped = %+v", v.Skipped)
	}
}

// TestDecisionDiagram: from the request, each guard in rule order; every
// question has a yes and a no; the one focal node is Allowed.
func TestDecisionDiagram(t *testing.T) {
	v := build(t)
	if !slices.Equal(v.Names, []string{"operations/invoice-refund", "operations/invoice-total"}) {
		t.Fatalf("diagrams = %q", v.Names)
	}
	d := v.Diagrams["operations/invoice-refund"]
	labels := map[string]string{}
	var focal []string
	for _, n := range d.Nodes {
		labels[n.ID] = n.Label
		if n.Class == d2.ClassFocal {
			focal = append(focal, n.Label)
		}
	}
	if !slices.Equal(focal, []string{"Allowed, rate limits applied"}) {
		t.Errorf("focal nodes = %q", focal)
	}
	if first := d.Nodes[0].Label; first != "Request Invoice.refund" {
		t.Errorf("first node = %q, want the request", first)
	}
	var path []string
	for _, e := range d.Edges {
		path = append(path, labels[e.From]+" -"+e.Label+"-> "+labels[e.To])
	}
	for _, want := range []string{
		"Request Invoice.refund --> the caller is signed in?",
		"the caller is signed in? -no-> Denied",
		"the caller is signed in? -yes-> the caller is the user concerned?",
		"the caller is the user concerned? -yes-> Allowed, rate limits applied",
	} {
		if !slices.Contains(path, want) {
			t.Errorf("diagram lacks %q:\n%s", want, strings.Join(path, "\n"))
		}
	}
}

// TestPage: the page links each diagram, its SVG when drawn, else its
// source, and matches the golden (UPDATE_GOLDEN=1 rewrites it).
func TestPage(t *testing.T) {
	v := build(t)
	pictures, err := d2.Renderer{}.Draw(t.Context(), v.Names, v.Diagrams)
	if err != nil {
		t.Fatal(err)
	}
	pictures[0].SVG, pictures[0].DarkSVG = []byte("<svg/>"), []byte("<svg/>")
	files, err := v.Files(pictures)
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["index.html"])
	for _, want := range []string{
		`<a href="operations/invoice-refund.svg">`, `<a href="operations/invoice-total.d2">`,
		"rate-limited: rate_limit ratelimit.ByUser key=refund count=5 window=time.Hour",
		"<li><strong>Invoice.pdf</strong>: guard names no check</li>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, name := range []string{"operations/invoice-refund.svg", "operations/invoice-total.d2"} {
		if files[name] == nil {
			t.Errorf("no %s", name)
		}
	}
	golden := goldenDir + "index.golden.html"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err = os.WriteFile(golden, files["index.html"], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if page != string(want) {
		t.Errorf("index.html differs from %s (UPDATE_GOLDEN=1 to accept)", golden)
	}
}
