package main

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// Access decisions often read tables rather than code: graphql-gateway's
// per-operation rate limits (_rateLimits), the mutations exempt from CSRF
// (_csrfExempt), the accounts exempt from a limit, the old mobile opnames.
// A table read by a check gets rules of its own, cited on its entries, so
// the values the decision depends on are in the spec and grounded.

// tableFacts returns the rules of the tables the service's checks read:
// one per entry of a rate-limit table (its limits), and one per lookup
// table read by a policy, a function a policy calls, or a rate-limit
// function (its entries).
func tableFacts(sources []source, found []policy, w *wording) []Fact {
	idx := indexFuncs(sources)
	limited := rateLimitReaders(sources)
	var facts []Fact
	rateTables := make(map[string]bool) // "pkg.Table"
	for _, rl := range limited {
		facts = append(facts, rl.facts(w)...)
		rateTables[rl.reader.file.file.Name.Name+"."+rl.table.name] = true
	}
	seen := make(map[*ast.CompositeLit]bool)
	for _, reader := range tableReaders(found, limited, idx) {
		for _, t := range tablesRead(reader, sources) {
			if seen[t.lit] || rateTables[reader.file.file.Name.Name+"."+t.name] {
				continue
			}
			seen[t.lit] = true
			facts = append(facts, t.facts(reader, w)...)
		}
	}
	return facts
}

// table is a map literal: a package variable or a local of its reader.
type table struct {
	name  string
	lit   *ast.CompositeLit
	value ast.Expr // the map's value type
	file  *source
}

// tableReaders lists the functions whose tables matter, in order:
// policies, the functions of their package they call, rate-limit readers.
func tableReaders(found []policy, limited []rateTable, idx funcIndex) []funcDecl {
	var readers []funcDecl
	add := func(d funcDecl) {
		for _, r := range readers {
			if r.fn == d.fn {
				return
			}
		}
		readers = append(readers, d)
	}
	for _, p := range found {
		add(p.decl)
		pkg := p.decl.file.file.Name.Name
		ast.Inspect(p.decl.fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if ident, isIdent := call.Fun.(*ast.Ident); isIdent {
					if d, isFunc := idx.one(pkg + "." + ident.Name); isFunc {
						add(d)
					}
				}
			}
			return true
		})
	}
	for _, rl := range limited {
		add(rl.reader)
	}
	return readers
}

// tablesRead returns the map literals reader reads: package variables of
// its package it names, and locals it declares, in order of first use.
func tablesRead(reader funcDecl, sources []source) []table {
	pkg := reader.file.file.Name.Name
	vars := make(map[string]table)
	for i := range sources {
		if sources[i].file.Name.Name != pkg {
			continue
		}
		for _, v := range packageVars(sources[i].file) {
			if lit, value, ok := mapLiteral(v.value); ok {
				vars[v.name] = table{name: v.name, lit: lit, value: value, file: &sources[i]}
			}
		}
	}
	var found []table
	ast.Inspect(reader.fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if name, ok := soleAssignee(x); ok {
				if lit, value, isMap := mapLiteral(x.Rhs[0]); isMap {
					found = append(
						found,
						table{name: name, lit: lit, value: value, file: reader.file},
					)
					return false
				}
			}
		case *ast.Ident:
			if t, ok := vars[x.Name]; ok {
				found = append(found, t)
				delete(vars, x.Name)
			}
		}
		return true
	})
	return found
}

// mapLiteral returns e as a map literal keyed by strings, and its value
// type.
func mapLiteral(e ast.Expr) (*ast.CompositeLit, ast.Expr, bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, nil, false
	}
	m, isMap := lit.Type.(*ast.MapType)
	if !isMap {
		return nil, nil, false
	}
	key, isIdent := m.Key.(*ast.Ident)
	return lit, m.Value, isIdent && key.Name == goString
}

// facts renders a lookup table read by reader: one rule for a set
// (map[string]bool, keys held true) or a mapping (map[string]string), one
// per key for lists (map[string][]string). Other tables say nothing a rule
// can state without knowing their use.
func (t *table) facts(reader funcDecl, w *wording) []Fact {
	switch v := t.value.(type) {
	case *ast.Ident:
		switch v.Name {
		case goBool:
			keys := t.keys(func(value ast.Expr) bool {
				ident, ok := value.(*ast.Ident)
				return ok && ident.Name == goTrue
			})
			if len(keys) == 0 {
				return nil
			}
			text := fmt.Sprintf(w.tableSetFmt, quoteAll(keys), t.name, reader.fn.Name.Name)
			return []Fact{t.rule(t.name, t.lit, text, w)}
		case goString:
			text := fmt.Sprintf(w.tableMapFmt, t.pairs(w), t.name, reader.fn.Name.Name)
			return []Fact{t.rule(t.name, t.lit, text, w)}
		}
	case *ast.ArrayType:
		if ident, ok := v.Elt.(*ast.Ident); ok && ident.Name == goString {
			return t.listFacts(reader, w)
		}
	}
	return nil
}

// keys returns the string keys whose value keep accepts, in order.
func (t *table) keys(keep func(ast.Expr) bool) []string {
	var keys []string
	for _, elt := range t.lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok && keep(kv.Value) {
			if k, isString := stringLit(kv.Key); isString {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// pairs renders a map[string]string: "`a` to `b`, `c` to `""`".
func (t *table) pairs(w *wording) string {
	var parts []string
	for _, elt := range t.lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		k, isKey := stringLit(kv.Key)
		v, isValue := stringLit(kv.Value)
		if isKey && isValue {
			parts = append(parts, fmt.Sprintf(w.pairFmt, quote(k), quote(v)))
		}
	}
	return strings.Join(parts, ", ")
}

// listFacts renders a map[string][]string, one rule per key.
func (t *table) listFacts(reader funcDecl, w *wording) []Fact {
	var facts []Fact
	for _, elt := range t.lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		k, isKey := stringLit(kv.Key)
		list, isList := kv.Value.(*ast.CompositeLit)
		if !isKey || !isList {
			continue
		}
		var values []string
		for _, e := range list.Elts {
			if s, isString := stringLit(e); isString {
				values = append(values, s)
			}
		}
		if len(values) == 0 {
			continue
		}
		text := fmt.Sprintf(w.tableListFmt, quoteAll(values), quote(k), t.name, reader.fn.Name.Name)
		facts = append(facts, t.rule(t.name+"["+k+"]", kv, text, w))
	}
	return facts
}

// rule makes the rule of a table, or of one entry, cited on node.
func (t *table) rule(name string, node ast.Node, requirement string, w *wording) Fact {
	start := t.file.fset.Position(node.Pos()).Line
	end := t.file.fset.Position(node.End()).Line
	return tableRule(
		t.file,
		start,
		end,
		"table-"+name,
		fmt.Sprintf(w.tableTitleFmt, name),
		requirement,
	)
}

// tableRule makes a ubiquitous mechanical rule cited on lines start-end of
// file.
func tableRule(file *source, start, end int, slug, title, requirement string) Fact {
	return Fact{
		Kind: kindRule,
		ID:   fmt.Sprintf("rule.webapp.table.%s:%d-%d", file.name, start, end),
		Attrs: map[string]string{
			attrSlug:        slugify(slug),
			attrTitle:       title,
			attrRequirement: requirement,
			attrEARS:        earsUbiquitous,
			attrNature:      natureBusiness,
			attrDomain:      dirDomain(file.name),
		},
		Source:    Ref{Path: file.name, Lines: lines(start, end)},
		Origin:    origin,
		Certainty: certain,
	}
}

// rateTable is a table of rate limits a function reads: the limiter calls
// of reader take their count and window from fields of an entry of table
// (rules := _rateLimits[operationName]; ratelimit.ByIP(…,
// ratelimit.MaxRequestsPerWindow(rules.maxIPCount), …)).
type rateTable struct {
	reader funcDecl
	table  table
	limits []tableLimit // in call order
}

// tableLimit is one limiter call reading an entry's fields.
type tableLimit struct {
	scope         string // the limiter: ByIP, ByAccount…
	count, window string // the fields it reads
}

// rateLimitReaders finds the functions whose limiter calls read a rate
// table, in source order.
func rateLimitReaders(sources []source) []rateTable {
	var found []rateTable
	for i := range sources {
		s := &sources[i]
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if rt, reads := rateTableOf(funcDecl{file: s, fn: fn}, sources); reads {
				found = append(found, rt)
			}
		}
	}
	return found
}

// rateTableOf returns the rate table reader's limiter calls read, if any.
func rateTableOf(reader funcDecl, sources []source) (rateTable, bool) {
	var (
		limits []tableLimit
		entry  string // the local holding the entry: rules
	)
	ast.Inspect(reader.fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isRateLimit(call) {
			return true
		}
		count, countOK := field(option(call, "MaxRequestsPerWindow"))
		window, windowOK := field(option(call, "LimitWindow"))
		if countOK && windowOK && count[0] == window[0] && (entry == "" || entry == count[0]) {
			entry = count[0]
			limits = append(limits, tableLimit{
				scope:  calledName(call),
				count:  count[1],
				window: window[1],
			})
		}
		return true
	})
	if len(limits) == 0 {
		return rateTable{}, false
	}
	name, indexed := indexedTable(reader.fn.Body, entry)
	if !indexed {
		return rateTable{}, false
	}
	for _, t := range tablesRead(reader, sources) {
		if t.name == name {
			return rateTable{reader: reader, table: t, limits: limits}, true
		}
	}
	return rateTable{}, false
}

// field returns the local and field of "local.field".
func field(e ast.Expr) ([2]string, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return [2]string{}, false
	}
	x, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return [2]string{}, false
	}
	return [2]string{x.Name, sel.Sel.Name}, true
}

// indexedTable returns the table local is read from: "local := T[k]".
func indexedTable(body *ast.BlockStmt, local string) (string, bool) {
	name := ""
	ast.Inspect(body, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok {
			name = cmp.Or(name, indexedBy(assign, local))
		}
		return name == ""
	})
	return name, name != ""
}

// indexedBy returns T when assign is "local := T[k]"; "" otherwise.
func indexedBy(assign *ast.AssignStmt, local string) string {
	lhs, isSole := soleAssignee(assign)
	if !isSole || lhs != local {
		return ""
	}
	index, isIndex := assign.Rhs[0].(*ast.IndexExpr)
	if !isIndex {
		return ""
	}
	if t, isIdent := index.X.(*ast.Ident); isIdent {
		return t.Name
	}
	return ""
}

// facts renders one rule per entry of a rate table that sets a limit:
// "The system shall limit `createProgram` to 300 requests per hour by IP
// and to 25 requests per 2 hours by account." A limiter whose count the
// entry leaves out does not apply to it (the reader checks count > 0).
func (rt *rateTable) facts(w *wording) []Fact {
	r := renderer{w: w, fset: rt.table.file.fset}
	scopes := rateLimitScopes(w)
	var facts []Fact
	for _, elt := range rt.table.lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, isKey := stringLit(kv.Key)
		fields, isStruct := structFields(kv.Value)
		if !isKey || !isStruct {
			continue
		}
		var parts []string
		for _, l := range rt.limits {
			count, window := fields[l.count], fields[l.window]
			if count == nil || window == nil {
				continue
			}
			parts = append(parts, fmt.Sprintf(w.tableLimitFmt,
				fmt.Sprintf(w.limitFmt, r.quantity(count), r.window(window)), scopes[l.scope]))
		}
		if len(parts) == 0 {
			continue
		}
		start := rt.table.file.fset.Position(kv.Pos()).Line
		end := rt.table.file.fset.Position(kv.End()).Line
		text := fmt.Sprintf(w.rateTableFmt, quote(key), strings.Join(parts, " "+w.alsoWord+" "))
		facts = append(facts, tableRule(rt.table.file, start, end,
			"ratelimit-"+key, fmt.Sprintf(w.rateTitleFmt, key), text))
	}
	return facts
}

// structFields returns the fields a struct literal sets, by name.
func structFields(e ast.Expr) (map[string]ast.Expr, bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	fields := make(map[string]ast.Expr)
	for _, elt := range lit.Elts {
		if kv, isKV := elt.(*ast.KeyValueExpr); isKV {
			if name, isIdent := kv.Key.(*ast.Ident); isIdent {
				fields[name.Name] = kv.Value
			}
		}
	}
	return fields, true
}

// window renders a limit window: a lone time unit in the singular (per
// minute), anything else as duration renders it.
func (r renderer) window(e ast.Expr) string {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if pkg, isPkg := sel.X.(*ast.Ident); isPkg && pkg.Name == "time" {
			if word, known := singularUnits(r.w)[sel.Sel.Name]; known {
				return word
			}
		}
	}
	return r.duration(e)
}

// singularUnits words the time package's units, singular.
func singularUnits(w *wording) map[string]string {
	return map[string]string{"Second": w.second, "Minute": w.minute, "Hour": w.hour}
}

// stringLit returns the value of a string literal.
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

// quote renders a value as code: `v`, and `""` for the empty string.
func quote(v string) string {
	if v == "" {
		return "`\"\"`"
	}
	return "`" + v + "`"
}

// quoteAll renders values as a list of code.
func quoteAll(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = quote(v)
	}
	return strings.Join(quoted, ", ")
}

// addTableTexts sets the phrases of table rules. The table and its reader
// are named unquoted: the rule cites the table's entries, where neither
// name is written, and grounding checks quoted values against them.
func addTableTexts(w *wording, tr translate) {
	w.tableTitleFmt = tr("Table d'accès : %s", "Access table: %s")
	w.tableSetFmt = tr(
		"Le systeme doit tenir %s dans %s, que lit %s.",
		"The system shall hold %s in %s, which %s reads.",
	)
	w.tableMapFmt = tr(
		"Le systeme doit associer %s dans %s, que lit %s.",
		"The system shall map %s in %s, which %s reads.",
	)
	w.pairFmt = tr("%s à %s", "%s to %s")
	w.tableListFmt = tr(
		"Le systeme doit tenir %s pour %s dans %s, que lit %s.",
		"The system shall hold %s for %s in %s, which %s reads.",
	)
	w.rateTableFmt = tr("Le systeme doit limiter %s à %s.", "The system shall limit %s to %s.")
	w.tableLimitFmt = tr("%s %s", "%s %s")
	w.alsoWord = tr("et à", "and to")
	w.second = tr("seconde", "second")
	w.minute = tr("minute", "minute")
	w.hour = tr("heure", "hour")
}
