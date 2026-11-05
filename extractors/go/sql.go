package main

import (
	"regexp"
	"strings"
)

// Table is one CREATE TABLE parsed from a schema file.
type Table struct {
	Name       string
	Columns    []string    // "name:type"
	References []Reference // foreign keys, in declaration order
	Line       int         // 1-based line of the CREATE TABLE keyword
}

// Reference is one foreign key of a table: its columns reference Table.
// NotNull when every one of them is declared NOT NULL or PRIMARY KEY.
type Reference struct {
	Columns []string
	Table   string
	NotNull bool
}

// String is the reference as the references attribute writes it:
// "kaid+assignment_id:assignment_students:not_null".
func (r Reference) String() string {
	null := "null"
	if r.NotNull {
		null = "not_null"
	}
	return strings.Join(r.Columns, "+") + ":" + r.Table + ":" + null
}

// referencesRe matches "REFERENCES <table>" in a column or a table
// constraint; the table name is group 1.
var referencesRe = regexp.MustCompile("(?i)\\breferences\\s+[`\"']?([a-zA-Z_][a-zA-Z0-9_.]*)")

// foreignKeyRe matches a table-level "FOREIGN KEY (a, b)"; the column
// list is group 1.
var foreignKeyRe = regexp.MustCompile(`(?i)\bforeign\s+key\s*\(([^)]*)\)`)

// notNullRe matches a column's NOT NULL or PRIMARY KEY.
var notNullRe = regexp.MustCompile(`(?i)\bnot\s+null\b|\bprimary\s+key\b`)

var createTableRe = regexp.MustCompile(`(?is)create\s+table\s+(?:if\s+not\s+exists\s+)?` +
	"[`\"']?([a-zA-Z_][a-zA-Z0-9_.]*)[`\"']?\\s*\\(")

// nonColumnStart matches lines that declare a constraint, not a column.
var nonColumnStart = regexp.MustCompile(
	`(?i)^(primary|foreign|unique|constraint|check|key|index|exclude)\b`,
)

// ParseTables extracts CREATE TABLE statements from one SQL source.
func ParseTables(sql string) []Table {
	var tables []Table
	for _, loc := range createTableRe.FindAllStringSubmatchIndex(sql, -1) {
		name := sql[loc[2]:loc[3]]
		body, ok := balancedBody(sql, loc[1]-1) // loc[1]-1 points at "("
		if !ok {
			continue
		}
		tables = append(tables, Table{
			Name:       strings.Trim(name, "`\"'"),
			Columns:    parseColumns(body),
			References: parseReferences(body),
			Line:       1 + strings.Count(sql[:loc[0]], "\n"),
		})
	}
	return tables
}

// balancedBody returns the text inside the parenthesis group starting at
// openIdx (which must index a '('), excluding the outer parentheses.
func balancedBody(s string, openIdx int) (string, bool) {
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i], true
			}
		}
	}
	return "", false
}

// lineCommentRe matches a SQL "--" comment to the end of its line.
var lineCommentRe = regexp.MustCompile(`--[^\n]*`)

// definitions splits a CREATE TABLE body into its top-level definitions
// (columns and table constraints), trimmed, respecting nested parentheses;
// "--" comments are dropped first, so neither their words nor their commas
// count.
func definitions(body string) []string {
	body = lineCommentRe.ReplaceAllString(body, "")
	var defs []string
	depth, start := 0, 0
	flush := func(end int) {
		if def := strings.TrimSpace(body[start:end]); def != "" {
			defs = append(defs, def)
		}
		start = end + 1
	}
	for i := range len(body) {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				flush(i)
			}
		}
	}
	flush(len(body))
	return defs
}

// parseReferences returns a CREATE TABLE body's foreign keys: inline
// "col type REFERENCES t" and table-level "FOREIGN KEY (a, b) REFERENCES
// t(...)". A table-level key is NOT NULL when each of its columns is.
func parseReferences(body string) []Reference {
	var refs []Reference
	notNull := map[string]bool{}
	var tableLevel []string
	for _, def := range definitions(body) {
		if nonColumnStart.MatchString(def) {
			tableLevel = append(tableLevel, def)
			continue
		}
		name := strings.Trim(strings.Fields(def)[0], "`\"'")
		notNull[name] = notNullRe.MatchString(def)
		if m := referencesRe.FindStringSubmatch(def); m != nil {
			refs = append(
				refs,
				Reference{Columns: []string{name}, Table: m[1], NotNull: notNull[name]},
			)
		}
	}
	for _, def := range tableLevel {
		fk, ref := foreignKeyRe.FindStringSubmatch(def), referencesRe.FindStringSubmatch(def)
		if fk == nil || ref == nil {
			continue
		}
		r := Reference{Table: ref[1], NotNull: true}
		for c := range strings.SplitSeq(fk[1], ",") {
			c = strings.Trim(strings.TrimSpace(c), "`\"'")
			r.Columns = append(r.Columns, c)
			r.NotNull = r.NotNull && notNull[c]
		}
		refs = append(refs, r)
	}
	return refs
}

// parseColumns splits a CREATE TABLE body into "name:type" column entries,
// skipping table-level constraints and respecting nested parentheses.
func parseColumns(body string) []string {
	var columns []string
	for _, field := range definitions(body) {
		if nonColumnStart.MatchString(field) {
			continue
		}
		fields := strings.Fields(field)
		colName := strings.Trim(fields[0], "`\"'")
		colType := ""
		if len(fields) > 1 {
			colType = strings.ToLower(strings.Trim(fields[1], "`\"',"))
		}
		if colType == "" {
			columns = append(columns, colName)
		} else {
			columns = append(columns, colName+":"+colType)
		}
	}
	return columns
}
