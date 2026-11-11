// Package alignment measures how a person's confirmed concepts line up
// with the code, without an LLM, once concept.Assign has placed the
// business rules:
//
//   - which relationships of the model the code shows (a reflexion model:
//     convergence, divergence, absence), from facts the code alone
//     produces: foreign keys between the concepts' tables, and direct calls
//     from code one concept's rules cite into code another's cite;
//   - how scattered each concept's rules are over directories, and how
//     tangled each directory is over concepts;
//   - how far the import graph's communities recover the concepts.
//
// Every figure is a description of the code against the model, not a
// quality score: none has been checked against outcomes. The rules'
// placement itself rests on their concept_evidence (entities touched,
// condition variables), which the report carries.
package alignment

import (
	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Relation is a relationship a confirmed model declares between two
// concepts, both qualified ("<scope>.<Entity>").
type Relation struct{ From, To string }

// Input is what Measure reads.
type Input struct {
	Nodes []graph.Node   // rules annotated by concept.Assign
	Facts []extract.Fact // the merged facts: table and call facts
	// Concepts are the confirmed models' concepts (domainmodel.Concepts).
	Concepts []concept.Concept
	Declared []Relation
	// Communities is each file's community of the import graph
	// (communities.Of), by path under --src.
	Communities map[string]int
}

// Report is Measure's result.
type Report struct {
	Pairs      []Pair   // sorted by A then B
	Scattering []Spread // per concept: the directories its rules cite
	Tangling   []Spread // per directory: the concepts of the rules citing it
	Recovery   Agreement
}

// Measure compares in's confirmed concepts with the code.
//
// Requires: in.Nodes annotated by concept.Assign; in.Concepts with distinct
// names.
// Ensures:  in is not modified; deterministic whatever the order of nodes,
// facts, concepts and relations.
func Measure(in *Input) Report {
	placed := placedRules(in.Nodes)
	return Report{
		Pairs:      pairs(placed, in),
		Scattering: scattering(placed),
		Tangling:   tangling(placed),
		Recovery:   recovery(placed, in.Communities),
	}
}

// placedRule is a rule concept.Assign placed, with its citations.
type placedRule struct {
	concept string
	sources []extract.Ref
}

// placedRules are the rules with a concept.
func placedRules(nodes []graph.Node) []placedRule {
	var placed []placedRule
	for i := range nodes {
		n := &nodes[i]
		if name := n.Extra[concept.ExtraConcept]; n.Type == graph.TypeRule && name != "" {
			placed = append(placed, placedRule{concept: name, sources: n.Sources})
		}
	}
	return placed
}
