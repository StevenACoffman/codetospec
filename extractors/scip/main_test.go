package main

import (
	"testing"

	"github.com/scip-code/scip/bindings/go/scip"
)

// syntheticIndex builds a one-document SCIP index with a function definition
// carrying a precise enclosing (body) range.
func syntheticIndex() *scip.Index {
	const sym = "scip-go gomod codetospec v1 `internal/billing`/Calculate()."
	return &scip.Index{
		Metadata: &scip.Metadata{ProjectRoot: "file:///repo"},
		Documents: []*scip.Document{
			{
				RelativePath: "internal/billing/calc.go",
				Language:     "Go",
				Symbols: []*scip.SymbolInformation{
					{
						Symbol:      sym,
						DisplayName: "Calculate",
						Kind:        scip.SymbolInformation_Function,
						SignatureDocumentation: &scip.Signature{
							Text: "func Calculate(amount float64) float64",
						},
					},
				},
				Occurrences: []*scip.Occurrence{
					{
						Symbol:         sym,
						SymbolRoles:    int32(scip.SymbolRole_Definition),
						Range:          []int32{10, 5, 10, 14}, // the identifier
						EnclosingRange: []int32{10, 0, 24, 1},  // the whole body → lines 11-25
					},
					{
						Symbol:      sym,
						SymbolRoles: 0, // a plain reference, must be ignored
						Range:       []int32{40, 2, 40, 11},
					},
				},
			},
		},
	}
}

func TestConvertEmitsPreciseSymbol(t *testing.T) {
	facts := convert(syntheticIndex(), "")
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want 1 (only the definition, not the reference)", len(facts))
	}
	f := facts[0]
	if f.Kind != "symbol" || f.Origin != "scip" || f.Certainty != "proved" {
		t.Errorf("fact envelope = %+v", f)
	}
	if f.Source.Path != "internal/billing/calc.go" || f.Source.Lines != "11-25" {
		t.Errorf("source = %+v, want internal/billing/calc.go:11-25 (enclosing range)", f.Source)
	}
	if f.Attrs["name"] != "Calculate" || f.Attrs["kind"] != "function" {
		t.Errorf("attrs name/kind = %q/%q", f.Attrs["name"], f.Attrs["kind"])
	}
	if f.Attrs["precise"] != "true" {
		t.Error("symbol fact must be marked precise")
	}
	if f.Attrs["signature"] != "func Calculate(amount float64) float64" {
		t.Errorf("signature = %q", f.Attrs["signature"])
	}
	if f.Attrs["namespace"] != "internal/billing" {
		t.Errorf("namespace = %q, want internal/billing", f.Attrs["namespace"])
	}
}

func TestConvertStripsRoot(t *testing.T) {
	facts := convert(syntheticIndex(), "internal")
	if len(facts) != 1 {
		t.Fatalf("facts = %d", len(facts))
	}
	if facts[0].Source.Path != "billing/calc.go" {
		t.Errorf("path = %q, want billing/calc.go (root stripped)", facts[0].Source.Path)
	}
}

func TestConvertFallsBackToIdentifierRange(t *testing.T) {
	idx := syntheticIndex()
	idx.Documents[0].Occurrences[0].EnclosingRange = nil // no body range
	facts := convert(idx, "")
	if len(facts) != 1 || facts[0].Source.Lines != "11-11" {
		t.Fatalf("want fallback to identifier line 11-11, got %+v", facts)
	}
}

func TestCitable(t *testing.T) {
	const pkg = "scip-go gomod example.com/m v1 `example.com/m/billing`/"
	cases := map[string]struct {
		symbol string
		want   bool
	}{
		"function":           {pkg + "Calculate().", true},
		"method":             {pkg + "Invoice#Total().", true},
		"type":               {pkg + "Invoice#", true},
		"package-level term": {pkg + "maxRetries.", true},
		"field":              {pkg + "Invoice#Amount.", false},
		"parameter":          {pkg + "Calculate().(amount)", false},
		"package":            {pkg, false},
		"local":              {"local 12", false},
		"unparsable":         {"not a symbol", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := citable(tc.symbol); got != tc.want {
				t.Fatalf("citable(%q) = %v, want %v", tc.symbol, got, tc.want)
			}
		})
	}
}
