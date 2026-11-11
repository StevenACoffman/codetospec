package main

import (
	"regexp"
	"slices"
	"sort"
	"strings"
)

// The SQL reader is regexp-based on purpose: a real parser (pg_query_go)
// would read one dialect, need cgo, and drop the comments that describe
// tables and columns. It reads the statements that shape a schema and
// skips everything else.

// Table is one table as the SQL files leave it: declared by a CREATE
// TABLE, then changed by the ALTER TABLE statements that follow.
type Table struct {
	Name       string
	Columns    []string    // "name:type"
	References []Reference // foreign keys, in declaration order
	PrimaryKey []string    // its primary key's columns; nil when none is declared
	// Description is the comment on the lines right above its CREATE
	// TABLE; ColumnDescriptions, per column, the comment trailing its
	// definition, else the one on the lines right above it.
	Description        string
	ColumnDescriptions map[string]string
	File               string // the source creating it, as apply was told
	Line               int    // 1-based line of the CREATE TABLE keyword
}

// Reference is one foreign key of a table: its columns reference Table.
// NotNull when every one of them is declared NOT NULL or is in the
// primary key.
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

// ParseTables reads one SQL source: the tables it creates, as its later
// statements leave them.
func ParseTables(sql string) []Table {
	s := newSchema()
	s.apply("", sql)
	return s.list()
}

// schema accumulates the tables of SQL sources applied in order, as a
// database running them would: migrations, or one schema file.
type schema struct {
	tables map[string]*table
	order  []string // table names, by first declaration
}

// table is a table being built: columns keep their NOT NULL, so a
// reference's is decided once every statement has run.
type table struct {
	name       string
	columns    []column
	references []Reference // NotNull decided by list
	primaryKey []string
	comment    string
	file       string
	line       int
}

type column struct {
	name, typ, comment string
	notNull            bool
}

func newSchema() *schema {
	return &schema{tables: map[string]*table{}}
}

var (
	createTableRe = regexp.MustCompile(`(?is)create\s+(?:(?:temporary|temp|unlogged)\s+)?` +
		`table\s+(?:if\s+not\s+exists\s+)?` + "[`\"']?([a-zA-Z_][a-zA-Z0-9_.]*)[`\"']?\\s*\\(")
	alterTableRe = regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?` +
		quotedName + `\s+(.*)$`)
	dropTableRe = regexp.MustCompile(`(?is)^drop\s+table\s+(?:if\s+exists\s+)?(.*?)` +
		`(?:\s+(?:cascade|restrict))?$`)
)

// quotedName matches a possibly quoted, possibly schema-qualified name;
// group 1 is the name.
const quotedName = "[`\"']?([a-zA-Z_][a-zA-Z0-9_.]*)[`\"']?"

// apply runs the statements of SQL source file against s. A table created twice
// keeps its first declaration; ALTER TABLE and DROP TABLE change the
// tables declared so far; other statements are ignored.
//
// Requires: sources applied in the order they run (migration order).
// Ensures:  comments (descriptions aside) and rollback sections never
// change s.
func (s *schema) apply(file, sql string) {
	src := readSource(sql)
	for _, stmt := range src.statements() {
		text := src.code[stmt.start:stmt.end]
		switch {
		case alterTableRe.MatchString(text):
			m := alterTableRe.FindStringSubmatchIndex(text)
			if t := s.tables[unquote(text[m[2]:m[3]])]; t != nil {
				s.alter(t, &src, stmt.start+m[4], stmt.start+m[5])
			}
		case dropTableRe.MatchString(text):
			m := dropTableRe.FindStringSubmatch(text)
			for name := range strings.SplitSeq(m[1], ",") {
				s.drop(unquote(strings.TrimSpace(name)))
			}
		default:
			for _, loc := range createTableRe.FindAllStringSubmatchIndex(text, -1) {
				s.create(&src, file, stmt.start+loc[0], unquote(text[loc[2]:loc[3]]),
					stmt.start+loc[1]-1)
			}
		}
	}
}

// create declares the table of file whose CREATE TABLE starts at start
// and whose body opens at open (an index of "(").
func (s *schema) create(src *source, file string, start int, name string, open int) {
	end, ok := balanced(src.code, open)
	if !ok || s.tables[name] != nil {
		return
	}
	line := src.lineOf(start)
	t := &table{name: name, file: file, line: line, comment: src.above(line)}
	var constraints []definition
	for _, def := range src.definitions(open+1, end) {
		if nonColumnStart.MatchString(def.text) {
			constraints = append(constraints, def)
			continue
		}
		t.addColumn(src, def)
	}
	for _, def := range constraints {
		t.addConstraint(def.text)
	}
	s.tables[name] = t
	s.order = append(s.order, name)
}

// alter applies the comma-separated actions of an ALTER TABLE, the text
// between start and end.
func (s *schema) alter(t *table, src *source, start, end int) {
	for _, def := range src.definitions(start, end) {
		action := def.text
		switch m := alterActionRe.FindStringSubmatch(action); {
		case m == nil:
		case strings.EqualFold(m[1], "add"):
			rest := action[len(m[0]):]
			if nonColumnStart.MatchString(rest) {
				t.addConstraint(rest)
			} else {
				def.text = rest
				t.addColumn(src, def)
			}
		case strings.EqualFold(m[1], "drop"):
			t.dropColumn(action[len(m[0]):])
		case strings.EqualFold(m[1], "alter"):
			t.alterColumn(action[len(m[0]):])
		case strings.EqualFold(m[1], "rename"):
			s.rename(t, action[len(m[0]):])
		}
	}
}

// alterActionRe matches an ALTER TABLE action's verb (group 1) and the
// optional COLUMN and IF [NOT] EXISTS words after it.
var alterActionRe = regexp.MustCompile(
	`(?is)^(add|drop|alter|rename)\s+(?:column\s+)?(?:if\s+(?:not\s+)?exists\s+)?`,
)

// drop removes a table.
func (s *schema) drop(name string) {
	if s.tables[name] == nil {
		return
	}
	delete(s.tables, name)
	s.order = slices.DeleteFunc(s.order, func(n string) bool { return n == name })
}

var (
	renameColumnRe = regexp.MustCompile(`(?is)^` + quotedName + `\s+to\s+` + quotedName + `$`)
	renameTableRe  = regexp.MustCompile(`(?is)^to\s+` + quotedName + `$`)
)

// rename applies RENAME TO u (the table's new name, which the references
// to it follow) or RENAME [COLUMN] a TO b.
func (s *schema) rename(t *table, rest string) {
	if table := renameTableRe.FindStringSubmatch(rest); table != nil {
		s.renameTable(t, unquote(table[1]))
	} else if col := renameColumnRe.FindStringSubmatch(rest); col != nil {
		t.renameColumn(unquote(col[1]), unquote(col[2]))
	}
}

func (s *schema) renameTable(t *table, name string) {
	old := t.name
	if s.tables[name] != nil {
		return
	}
	delete(s.tables, old)
	t.name, s.tables[name] = name, t
	s.order[slices.Index(s.order, old)] = name
	for _, other := range s.tables {
		for i := range other.references {
			if other.references[i].Table == old {
				other.references[i].Table = name
			}
		}
	}
}

func (t *table) renameColumn(old, name string) {
	for i := range t.columns {
		if t.columns[i].name == old {
			t.columns[i].name = name
		}
	}
	renamed := func(cols []string) {
		for i := range cols {
			if cols[i] == old {
				cols[i] = name
			}
		}
	}
	renamed(t.primaryKey)
	for i := range t.references {
		renamed(t.references[i].Columns)
	}
}

var (
	// referencesRe matches "REFERENCES <table>" in a column or a table
	// constraint; the table name is group 1.
	referencesRe = regexp.MustCompile("(?i)\\breferences\\s+[`\"']?([a-zA-Z_][a-zA-Z0-9_.]*)")
	// foreignKeyRe matches a table-level "FOREIGN KEY (a, b)"; the column
	// list is group 1.
	foreignKeyRe = regexp.MustCompile(`(?i)\bforeign\s+key\s*\(([^)]*)\)`)
	// primaryKeyRe matches a table-level "PRIMARY KEY (a, b)"; the column
	// list is group 1.
	primaryKeyRe = regexp.MustCompile(`(?i)\bprimary\s+key\s*\(([^)]*)\)`)
	notNullRe    = regexp.MustCompile(`(?i)\bnot\s+null\b`)
	inlinePKRe   = regexp.MustCompile(`(?i)\bprimary\s+key\b`)
	// nonColumnStart matches a definition that declares a constraint, not
	// a column.
	nonColumnStart = regexp.MustCompile(
		`(?i)^(primary|foreign|unique|constraint|check|key|index|exclude)\b`,
	)
)

// addColumn adds the column def declares, with its inline NOT NULL,
// PRIMARY KEY and REFERENCES; a column already there is kept.
func (t *table) addColumn(src *source, def definition) {
	fields := strings.Fields(def.text)
	name := unquote(fields[0])
	if slices.ContainsFunc(t.columns, func(c column) bool { return c.name == name }) {
		return
	}
	c := column{name: name, notNull: notNullRe.MatchString(def.text), comment: src.commentOf(def)}
	if len(fields) > 1 {
		c.typ = strings.ToLower(strings.Trim(fields[1], "`\"',"))
	}
	t.columns = append(t.columns, c)
	if inlinePKRe.MatchString(def.text) {
		t.primaryKey = []string{name}
	}
	if m := referencesRe.FindStringSubmatch(def.text); m != nil {
		t.references = append(t.references, Reference{Columns: []string{name}, Table: m[1]})
	}
}

// addConstraint records a table-level PRIMARY KEY or FOREIGN KEY, named
// (CONSTRAINT n …) or not; other constraints are ignored.
func (t *table) addConstraint(def string) {
	if m := primaryKeyRe.FindStringSubmatch(def); m != nil {
		t.primaryKey = columnList(m[1])
		return
	}
	fk, ref := foreignKeyRe.FindStringSubmatch(def), referencesRe.FindStringSubmatch(def)
	if fk != nil && ref != nil {
		t.references = append(t.references, Reference{Columns: columnList(fk[1]), Table: ref[1]})
	}
}

// dropColumn removes a column, with the keys that use it.
func (t *table) dropColumn(rest string) {
	fields := strings.Fields(rest)
	if len(fields) == 0 || strings.EqualFold(fields[0], "constraint") {
		return
	}
	name := unquote(fields[0])
	t.columns = slices.DeleteFunc(t.columns, func(c column) bool { return c.name == name })
	if slices.Contains(t.primaryKey, name) {
		t.primaryKey = nil
	}
	t.references = slices.DeleteFunc(t.references, func(r Reference) bool {
		return slices.Contains(r.Columns, name)
	})
}

var (
	setNotNullRe = regexp.MustCompile(`(?is)^` + quotedName + `\s+(set|drop)\s+not\s+null\b`)
	setTypeRe    = regexp.MustCompile(`(?is)^` + quotedName + `\s+(?:set\s+data\s+)?type\s+(\S+)`)
)

// alterColumn applies ALTER [COLUMN] c SET|DROP NOT NULL and
// [SET DATA] TYPE t; other changes (defaults, statistics) are ignored.
func (t *table) alterColumn(rest string) {
	if m := setNotNullRe.FindStringSubmatch(rest); m != nil {
		for i := range t.columns {
			if t.columns[i].name == unquote(m[1]) {
				t.columns[i].notNull = strings.EqualFold(m[2], "set")
			}
		}
		return
	}
	if m := setTypeRe.FindStringSubmatch(rest); m != nil {
		for i := range t.columns {
			if t.columns[i].name == unquote(m[1]) {
				t.columns[i].typ = strings.ToLower(strings.Trim(m[2], "`\"',"))
			}
		}
	}
}

// list is the tables in declaration order, each reference NOT NULL when
// every one of its columns is NOT NULL or in the primary key.
func (s *schema) list() []Table {
	tables := make([]Table, 0, len(s.order))
	for _, name := range s.order {
		t := s.tables[name]
		out := Table{
			Name: t.name, PrimaryKey: t.primaryKey, Description: t.comment,
			File: t.file, Line: t.line,
		}
		notNull := map[string]bool{}
		for _, c := range t.columns {
			entry := c.name
			if c.typ != "" {
				entry += ":" + c.typ
			}
			out.Columns = append(out.Columns, entry)
			notNull[c.name] = c.notNull || slices.Contains(t.primaryKey, c.name)
			if c.comment != "" {
				if out.ColumnDescriptions == nil {
					out.ColumnDescriptions = map[string]string{}
				}
				out.ColumnDescriptions[c.name] = c.comment
			}
		}
		for _, r := range t.references {
			r.Columns = slices.Clone(r.Columns)
			r.NotNull = !slices.ContainsFunc(r.Columns, func(c string) bool { return !notNull[c] })
			out.References = append(out.References, r)
		}
		tables = append(tables, out)
	}
	return tables
}

// columnList splits "a, `b`" into its column names.
func columnList(list string) []string {
	var cols []string
	for c := range strings.SplitSeq(list, ",") {
		if c = unquote(strings.TrimSpace(c)); c != "" {
			cols = append(cols, c)
		}
	}
	return cols
}

func unquote(name string) string {
	return strings.Trim(name, "`\"'")
}

// source is one SQL source with its comments blanked out (same offsets,
// so lines stay right) and kept by line.
type source struct {
	code       string
	lineStarts []int           // offset of each line's first byte
	comments   map[int]comment // by 1-based line
}

// comment is a "--" comment; ownLine when nothing but blanks precede it
// on its line.
type comment struct {
	text    string
	ownLine bool
}

// downMarkerRe and upMarkerRe match the lines opening a migration file's
// rollback section (goose, dbmate) and its forward one; directiveRe, a
// comment that is a migration tool's directive, never a description.
var (
	downMarkerRe = regexp.MustCompile(`(?i)^--\s*(\+goose\s+down|migrate:down)\b`)
	upMarkerRe   = regexp.MustCompile(`(?i)^--\s*(\+goose\s+up|migrate:up)\b`)
	directiveRe  = regexp.MustCompile(`(?i)^(\+goose\b|migrate:)`)
)

// readSource blanks sql's rollback sections and comments (outside quoted
// strings), keeping the comments by line.
func readSource(sql string) source {
	src := source{comments: map[int]comment{}}
	code := []byte(sql)
	down, offset := false, 0
	for line := range strings.SplitAfterSeq(sql, "\n") {
		src.lineStarts = append(src.lineStarts, offset)
		switch trimmed := strings.TrimSpace(line); {
		case downMarkerRe.MatchString(trimmed):
			down = true
		case upMarkerRe.MatchString(trimmed):
			down = false
		}
		if down {
			blank(code[offset : offset+len(line)])
		}
		offset += len(line)
	}
	var quote byte
	for i := 0; i < len(code); i++ {
		switch ch := code[i]; {
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '-' && i+1 < len(code) && code[i+1] == '-':
			end := i + strings.IndexByte(sql[i:]+"\n", '\n')
			line := src.lineOf(i)
			text := strings.TrimSpace(sql[i+2 : end])
			if !directiveRe.MatchString(text) {
				src.comments[line] = comment{
					text:    text,
					ownLine: strings.TrimSpace(sql[src.lineStarts[line-1]:i]) == "",
				}
			}
			blank(code[i:end])
			i = end - 1
		}
	}
	src.code = string(code)
	return src
}

// blank replaces b's bytes with spaces, newlines kept.
func blank(b []byte) {
	for i := range b {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

// lineOf is the 1-based line of offset.
func (s *source) lineOf(offset int) int {
	return sort.Search(len(s.lineStarts), func(i int) bool { return s.lineStarts[i] > offset })
}

// above is the own-line comments on the lines right above line, joined.
func (s *source) above(line int) string {
	var parts []string
	for l := line - 1; l > 0; l-- {
		c, ok := s.comments[l]
		if !ok || !c.ownLine {
			break
		}
		parts = append(parts, c.text)
	}
	slices.Reverse(parts)
	return strings.Join(parts, " ")
}

// span is a part of the code, by offsets.
type span struct{ start, end int }

// statements splits the code at the semicolons outside parentheses and
// quotes, each statement trimmed.
func (s *source) statements() []span {
	return s.split(0, len(s.code), ';')
}

// definition is one comma-separated part of a body or of an ALTER
// TABLE's actions, trimmed; last is whether no other part ends on its
// last line after it.
type definition struct {
	text       string
	start, end int
	last       bool
}

// definitions splits the code between start and end at the commas
// outside parentheses and quotes.
func (s *source) definitions(start, end int) []definition {
	parts := s.split(start, end, ',')
	defs := make([]definition, len(parts))
	for i, p := range parts {
		defs[i] = definition{text: s.code[p.start:p.end], start: p.start, end: p.end, last: true}
		if i > 0 && s.lineOf(parts[i-1].end-1) == s.lineOf(p.end-1) {
			defs[i-1].last = false
		}
	}
	return defs
}

// commentOf is def's description: the comment trailing its last line when
// no later definition ends there, else the own-line comments right above
// its first line.
func (s *source) commentOf(def definition) string {
	if c, ok := s.comments[s.lineOf(def.end-1)]; ok && !c.ownLine && def.last {
		return c.text
	}
	return s.above(s.lineOf(def.start))
}

// split cuts the code between start and end at sep outside parentheses
// and quotes, dropping blank parts and trimming the others.
func (s *source) split(start, end int, sep byte) []span {
	var parts []span
	depth, from := 0, start
	var quote byte
	flush := func(to int) {
		a, b := from, to
		for a < b && isBlank(s.code[a]) {
			a++
		}
		for b > a && isBlank(s.code[b-1]) {
			b--
		}
		if a < b {
			parts = append(parts, span{a, b})
		}
		from = to + 1
	}
	for i := start; i < end; i++ {
		ch := s.code[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case ch == sep && depth == 0:
			flush(i)
		}
	}
	flush(end)
	return parts
}

func isBlank(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

// balanced returns the index of the ")" closing the "(" at open.
func balanced(code string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(code); i++ {
		switch code[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}
