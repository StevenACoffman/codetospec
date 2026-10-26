package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// Attributes of a role rule, for consumers that join roles to the
// permissions services check (scripts/authzview.py).
const (
	attrRole         = "role"
	attrCapabilities = "capabilities" // comma-separated Go names
)

// role is one RegisterRoleDefinition(RoleDefinition{…}) of the service:
// granting a user the role grants them its capabilities (on the scope of
// the grant).
type role struct {
	file         string
	start, end   int
	name, desc   string
	capabilities []string // Go names: CanViewUserIdentity
}

// roleFacts returns one mechanical rule per role the service registers
// (the admin service's permissions/roles), cited on its definition.
func roleFacts(sources []source, w *wording) []Fact {
	var facts []Fact
	for i := range sources {
		for _, r := range rolesIn(&sources[i]) {
			facts = append(facts, roleFact(&r, w))
		}
	}
	return facts
}

// rolesIn finds the role definitions of one file.
func rolesIn(s *source) []role {
	var found []role
	ast.Inspect(s.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || calledName(call) != "RegisterRoleDefinition" || len(call.Args) != 1 {
			return true
		}
		def, isLit := call.Args[0].(*ast.CompositeLit)
		if !isLit {
			return true
		}
		r := role{
			file:  s.name,
			start: s.fset.Position(call.Pos()).Line,
			end:   s.fset.Position(call.End()).Line,
		}
		for _, elt := range def.Elts {
			kv, isKV := elt.(*ast.KeyValueExpr)
			key, isIdent := kv.Key.(*ast.Ident)
			if !isKV || !isIdent {
				continue
			}
			switch key.Name {
			case "Name":
				r.name = stringValue(kv.Value)
			case "Description":
				r.desc = stringValue(kv.Value)
			case "CapabilitySet":
				r.capabilities = capabilityNames(kv.Value)
			}
		}
		if r.name != "" {
			found = append(found, r)
		}
		return false
	})
	return found
}

// roleFact renders a role as an EARS rule.
func roleFact(r *role, w *wording) Fact {
	quoted := make([]string, len(r.capabilities))
	for i, c := range r.capabilities {
		quoted[i] = "`" + c + "`"
	}
	desc := ""
	if r.desc != "" {
		desc = " (" + r.desc + ")"
	}
	requirement := fmt.Sprintf(w.roleNoneFmt, r.name, desc)
	if len(quoted) > 0 {
		requirement = fmt.Sprintf(w.roleFmt, r.name, desc, strings.Join(quoted, ", "))
	}
	return Fact{
		Kind: kindRule,
		ID:   fmt.Sprintf("rule.webapp.role.%s:%d-%d", r.file, r.start, r.end),
		Attrs: map[string]string{
			attrSlug:         slugify("role-" + r.name),
			attrTitle:        fmt.Sprintf(w.roleTitleFmt, r.name),
			attrRequirement:  requirement,
			attrEARS:         earsUbiquitous,
			attrNature:       natureBusiness,
			attrDomain:       domainOf(path.Dir(r.file)),
			attrRole:         r.name,
			attrCapabilities: strings.Join(r.capabilities, ","),
		},
		Source:    Ref{Path: r.file, Lines: lines(r.start, r.end)},
		Origin:    origin,
		Certainty: certain,
	}
}

// calledName is the name a call calls: f in f(…) and pkg.f(…).
func calledName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// stringValue is the value of a string literal or of a concatenation of
// them ("Can view and edit users and access Devadmin support " +
// "functions."); "" for anything else.
func stringValue(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			if v, err := strconv.Unquote(x.Value); err == nil {
				return v
			}
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return stringValue(x.X) + stringValue(x.Y)
		}
	case *ast.ParenExpr:
		return stringValue(x.X)
	}
	return ""
}

// capabilityNames lists the Go names of a capability set literal
// ([]capabilities.Capability{capabilities.CanX, …}).
func capabilityNames(e ast.Expr) []string {
	set, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var names []string
	for _, elt := range set.Elts {
		switch x := elt.(type) {
		case *ast.SelectorExpr:
			names = append(names, x.Sel.Name)
		case *ast.Ident:
			names = append(names, x.Name)
		}
	}
	return names
}
