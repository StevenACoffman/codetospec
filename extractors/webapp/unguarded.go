package main

// kindUnguarded marks a resolver method the extractor saw with no guard
// and no marker (codetospec's extract.KindUnguarded): not a rule, since it
// states nothing about behavior, but the one way to tell "no check found"
// from "not a resolver" in the policy inventory.
const kindUnguarded = "unguarded"

// unguardedFact states that the public resolver method m of file, serving
// op, has no recognized guard, cited on the whole method. Its attributes
// are the operation and, for a root field, its route.
//
// Requires: m is public, has a schema field, and len(m.guards) == 0.
func unguardedFact(file string, m *resolverMethod, op *operation) Fact {
	attrs := map[string]string{attrOperation: op.name}
	if op.route != "" {
		attrs[attrRoute] = op.route
	}
	return Fact{
		Kind:      kindUnguarded,
		ID:        "unguarded.webapp." + file + ":" + lines(m.start, m.end),
		Attrs:     attrs,
		Source:    Ref{Path: file, Lines: lines(m.start, m.end)},
		Origin:    origin,
		Certainty: certain,
	}
}
