// Package grounding checks, without an LLM, that the concrete values a rule
// states — numbers and quoted strings in its requirement — appear in the code
// it cites. A value the cited lines do not contain is either hallucinated or
// cited off-target; when the same file contains it outside the citation, the
// result says where, which is usually the line the citation should cover.
//
// The check is deliberately one-sided: it can show that a stated value is
// missing, never that the behavior is right. A rule with no checkable value
// is reported StatusUnchecked, so "nothing to check" never reads as a pass.
package grounding

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Status is the outcome of checking one rule.
type Status string

const (
	// StatusGrounded means every stated value occurs in the cited lines.
	StatusGrounded Status = "grounded"
	// StatusUngrounded means at least one stated value is missing from the
	// cited lines.
	StatusUngrounded Status = "ungrounded"
	// StatusUnchecked means the requirement states no checkable value, or its
	// citations could not be read.
	StatusUnchecked Status = "unchecked"
)

// Citation is one cited line range together with its whole file, so a value
// missing from the range can still be located elsewhere in the file.
type Citation struct {
	Path  string
	Start int      // 1-based, inclusive
	End   int      // 1-based, inclusive
	File  []string // every line of the file
}

// Miss is one stated value absent from the cited lines.
type Miss struct {
	Literal string
	// Elsewhere lists "path:line" locations outside the citations where the
	// value does occur (at most maxElsewhere); empty when nowhere in the
	// cited files.
	Elsewhere []string
}

// Result is the outcome of Check.
type Result struct {
	Status  Status
	Missing []Miss
}

// maxElsewhere caps how many out-of-citation locations a Miss reports.
const maxElsewhere = 3

// Summary renders the missing values compactly, e.g.
// `30 @app/X.php:42; "PRO"` (an @ marks where the value occurs outside the
// citation). Empty when nothing is missing.
func (r Result) Summary() string {
	parts := make([]string, 0, len(r.Missing))
	for _, m := range r.Missing {
		s := m.Literal
		if len(m.Elsewhere) > 0 {
			s += " @" + strings.Join(m.Elsewhere, ",")
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

// Load reads the cited ranges of refs through roots.
//
// Requires: every ref.Lines is "A-B" with 1 <= A <= B.
// Ensures:  one Citation per ref, in order; End is clamped to the file length.
func Load(roots extract.Roots, refs []extract.Ref) ([]Citation, error) {
	files := make(map[string][]string)
	citations := make([]Citation, 0, len(refs))
	for _, ref := range refs {
		start, end, err := extract.ParseLines(ref.Lines)
		if err != nil {
			return nil, fmt.Errorf("grounding.Load: %s: %w", ref.File(), err)
		}
		lines, ok := files[ref.File()]
		if !ok {
			data, readErr := roots.ReadFile(ref)
			if readErr != nil {
				return nil, fmt.Errorf("grounding.Load: %w", readErr)
			}
			lines = strings.Split(string(data), "\n")
			files[ref.File()] = lines
		}
		// An end past the file is clamped (a range may name the final
		// newline's empty line), but a start past it cites nothing.
		if start > len(lines) {
			return nil, fmt.Errorf("grounding.Load: %s: lines %s exceed file (%d lines)",
				ref.File(), ref.Lines, len(lines))
		}
		citations = append(citations, Citation{
			Path:  ref.File(),
			Start: start,
			End:   min(end, len(lines)),
			File:  lines,
		})
	}
	return citations, nil
}

// Options tune how Check reads requirements and code.
type Options struct {
	// DecimalSeparator is how the requirement's language writes decimals
	// (see locale.Text.DecimalSeparator): with ',' a "1,000" reads 1.000,
	// with '.' it reads one thousand.
	DecimalSeparator byte
	// Constants maps identifiers as written in code to their numeric value
	// (see Constants), so "400" is found behind http.StatusBadRequest.
	Constants map[string]float64
}

// Constants indexes the numeric extract.KindConstant facts by name;
// non-numeric values are skipped.
func Constants(facts []extract.Fact) map[string]float64 {
	constants := make(map[string]float64)
	for _, f := range facts {
		if f.Kind != extract.KindConstant {
			continue
		}
		if v, err := strconv.ParseFloat(f.Attrs[extract.AttrValue], 64); err == nil {
			constants[f.Attrs[extract.AttrName]] = v
		}
	}
	return constants
}

// Check verifies that every number and quoted string stated in requirement
// occurs in the cited lines.
//
// Requires: opts.DecimalSeparator is ',' or '.'; empty inputs yield
// StatusUnchecked.
// Ensures:  Status is StatusUnchecked iff requirement states no literal or
// no citation is given; otherwise Missing lists, in order of appearance, each
// literal not found in any cited range.
func Check(requirement string, citations []Citation, opts Options) Result {
	literals := extractLiterals(requirement, opts.DecimalSeparator)
	if len(literals) == 0 || len(citations) == 0 {
		return Result{Status: StatusUnchecked}
	}
	var cited []string
	for _, c := range citations {
		cited = append(cited, c.File[c.Start-1:c.End]...)
	}
	result := Result{Status: StatusGrounded}
	for i := range literals {
		lit := &literals[i]
		if lit.foundIn(cited, opts.Constants) {
			continue
		}
		result.Status = StatusUngrounded
		result.Missing = append(result.Missing, Miss{
			Literal:   lit.text,
			Elsewhere: elsewhere(lit, citations, opts.Constants),
		})
	}
	return result
}

// elsewhere returns up to maxElsewhere "path:line" locations in the cited
// files, outside every cited range, whose line contains lit.
func elsewhere(lit *literal, citations []Citation, constants map[string]float64) []string {
	var locations []string
	seenFile := make(map[string]bool)
	for _, c := range citations {
		if seenFile[c.Path] {
			continue
		}
		seenFile[c.Path] = true
		for i, line := range c.File {
			n := i + 1
			if insideAny(c.Path, n, citations) || !lit.foundIn([]string{line}, constants) {
				continue
			}
			locations = append(locations, c.Path+":"+strconv.Itoa(n))
			if len(locations) == maxElsewhere {
				return locations
			}
		}
	}
	return locations
}

func insideAny(filePath string, line int, citations []Citation) bool {
	for _, c := range citations {
		if c.Path == filePath && c.Start <= line && line <= c.End {
			return true
		}
	}
	return false
}

// literal is one checkable value stated in a requirement.
type literal struct {
	text    string  // as written in the requirement
	number  bool    // numeric literal; value/percent apply
	value   float64 // numeric value
	percent bool    // written with a % sign: the code may hold value/100
	folded  string  // string literal, folded for matching
	// parts are the folded fixed parts of a string literal written with
	// <placeholders> ("fbsr_<appID>" → ["fbsr_"]); they must occur in order.
	parts []string
}

var (
	// claimNumberDecimalComma matches a number in prose that writes decimals
	// with a comma (French): "30", "1 000" (any space as a thousands
	// separator), "1,5" or "1.5", optionally followed by "%". The leading
	// group keeps digits glued to a word ("SHA256", "v2") out.
	claimNumberDecimalComma = regexp.MustCompile(
		`(?:^|[^\p{L}\p{N}_.,])(\d{1,3}(?:[ \x{00A0}\x{202F}]\d{3})+|\d+)([.,]\d+)?(\s*%)?`,
	)
	// claimNumberDecimalPoint matches a number in prose that writes decimals
	// with a point (English): "30", "1,000" or "1 000" (thousands
	// separators), "1.5", optionally followed by "%".
	claimNumberDecimalPoint = regexp.MustCompile(
		`(?:^|[^\p{L}\p{N}_.,])` +
			`(\d{1,3}(?:,\d{3})+|\d{1,3}(?:[ \x{00A0}\x{202F}]\d{3})+|\d+)` +
			`(\.\d+)?(\s*%)?`,
	)
	// claimQuoted matches text in double quotes, curly quotes, guillemets or
	// backticks.
	claimQuoted = regexp.MustCompile("\"([^\"]+)\"|“([^”]+)”|«\\s*([^»]+?)\\s*»|`([^`]+)`")
	// claimSingleQuoted matches 'text' not glued to letters, so French
	// elisions such as "l'abonné" are not read as quotes.
	claimSingleQuoted = regexp.MustCompile(`(?:^|[^\p{L}])'([^']+)'(?:[^\p{L}]|$)`)
	// codeNumber matches a numeric literal in source code, including digit
	// separators ("1_000") and decimals.
	codeNumber = regexp.MustCompile(`\d[\d_]*(?:\.\d+)?`)
	// codeIdent matches an identifier in source code, optionally qualified
	// by one package or receiver ("http.StatusBadRequest").
	codeIdent = regexp.MustCompile(`[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)?`)
	// placeholder matches a <name> slot in a quoted template string.
	placeholder = regexp.MustCompile(`<[^<>]+>`)
)

// located is a literal and its byte offset in the requirement.
type located struct {
	at  int
	lit literal
}

// extractLiterals returns the numbers and quoted strings stated in text, in
// order of appearance, without duplicates.
func extractLiterals(text string, decimalSeparator byte) []literal {
	found, quoteSpans := quotedLiterals(text)
	found = append(found, numberLiterals(text, quoteSpans, decimalSeparator)...)
	// The requirement reads left to right, and so should the report.
	slices.SortStableFunc(found, func(a, b located) int { return a.at - b.at })

	seen := make(map[string]bool)
	literals := make([]literal, 0, len(found))
	for _, f := range found {
		if !seen[f.lit.text] {
			seen[f.lit.text] = true
			literals = append(literals, f.lit)
		}
	}
	return literals
}

// quotedLiterals returns the quoted strings in text and the byte spans they
// occupy.
func quotedLiterals(text string) ([]located, [][]int) {
	var (
		found []located
		spans [][]int
	)
	for _, m := range claimQuoted.FindAllStringSubmatchIndex(text, -1) {
		spans = append(spans, m[:2])
		for g := 2; g < len(m); g += 2 {
			if m[g] < 0 {
				continue
			}
			if lit, ok := quoted(text[m[0]:m[1]], text[m[g]:m[g+1]]); ok {
				found = append(found, located{m[0], lit})
			}
		}
	}
	for _, m := range claimSingleQuoted.FindAllStringSubmatchIndex(text, -1) {
		inner := text[m[2]:m[3]]
		spans = append(spans, m[2:4])
		if lit, ok := quoted("'"+inner+"'", inner); ok {
			found = append(found, located{m[2], lit})
		}
	}
	return found, spans
}

// numberLiterals returns the numbers in text outside quoteSpans: a number
// inside a quoted string is checked as part of that string.
func numberLiterals(text string, quoteSpans [][]int, decimalSeparator byte) []located {
	claimNumber := claimNumberDecimalComma
	if decimalSeparator == '.' {
		claimNumber = claimNumberDecimalPoint
	}
	var found []located
	for _, m := range claimNumber.FindAllStringSubmatchIndex(text, -1) {
		inQuote := slices.ContainsFunc(quoteSpans, func(span []int) bool {
			return span[0] <= m[2] && m[2] < span[1]
		})
		if inQuote {
			continue
		}
		if lit, ok := number(text, m); ok {
			found = append(found, located{m[2], lit})
		}
	}
	return found
}

// minPartLen is the shortest fixed part of a <placeholder> template worth
// checking; shorter ones ("-", ".") match almost anything.
const minPartLen = 2

// quoted builds a string literal. A template with <placeholders> is checked
// through its fixed parts; ok is false when none is long enough to check.
func quoted(asWritten, inner string) (literal, bool) {
	lit := literal{text: asWritten, folded: fold(inner)}
	if !placeholder.MatchString(inner) {
		return lit, true
	}
	for _, part := range placeholder.Split(inner, -1) {
		if part = fold(part); len(part) >= minPartLen {
			lit.parts = append(lit.parts, part)
		}
	}
	return lit, len(lit.parts) > 0
}

// number builds a numeric literal from a claimNumber submatch index m: the
// integer part (m[2:4], thousands separators dropped), an optional decimal
// part (m[4:6], separator included) and an optional percent sign (m[6:8]).
// ok is false if the digits do not parse (out of float range).
func number(text string, m []int) (literal, bool) {
	asWritten := text[m[2]:m[3]]
	digits := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\u00A0' || r == '\u202F' || r == ',' {
			return -1
		}
		return r
	}, asWritten)
	if m[4] >= 0 {
		asWritten += text[m[4]:m[5]]
		digits += "." + text[m[4]+1:m[5]]
	}
	value, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return literal{}, false
	}
	lit := literal{text: asWritten, number: true, value: value}
	if m[6] >= 0 {
		lit.percent = true
		lit.text += "%"
	}
	return lit, true
}

// foundIn reports whether lit occurs in any of lines; a number also occurs
// as an identifier that constants binds to its value.
func (l *literal) foundIn(lines []string, constants map[string]float64) bool {
	if !l.number {
		return l.textIn(fold(strings.Join(lines, "\n")))
	}
	for _, line := range lines {
		for _, tok := range codeNumber.FindAllString(line, -1) {
			v, err := strconv.ParseFloat(strings.ReplaceAll(tok, "_", ""), 64)
			if err == nil && l.matches(v) {
				return true
			}
		}
		for _, ident := range codeIdent.FindAllString(line, -1) {
			if v, ok := constants[ident]; ok && l.matches(v) {
				return true
			}
		}
	}
	return false
}

// matches reports whether the code value v is the stated number.
func (l *literal) matches(v float64) bool {
	return sameNumber(v, l.value) || (l.percent && sameNumber(v*percentScale, l.value))
}

// textIn reports whether a string literal occurs in folded code: verbatim, or
// for a template, its fixed parts in order.
func (l *literal) textIn(code string) bool {
	if len(l.parts) == 0 {
		return strings.Contains(code, l.folded)
	}
	for _, part := range l.parts {
		i := strings.Index(code, part)
		if i < 0 {
			return false
		}
		code = code[i+len(part):]
	}
	return true
}

// percentScale converts a ratio in code (0.1) to the percentage a
// requirement states (10 %).
const percentScale = 100

// sameNumber compares with a relative tolerance, so 0.1*100 equals 10.
func sameNumber(a, b float64) bool {
	const tolerance = 1e-9
	return math.Abs(a-b) <= tolerance*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// codePunct are the characters code may or may not space around: a
// formatter writes "a + b" where the source has "a+b".
const codePunct = "+-*/%=<>!&|^,;:()[]{}"

// fold lowercases s, unifies typographic quotes and collapses whitespace, so
// a quoted string matches code regardless of case and layout. Space next to
// code punctuation goes, and so does a trailing comma: a call split over
// lines ("f(\n\tktx,\n\tx,\n)") reads as written on one ("f(ktx, x)"), and
// "10*time.Minute" as "10 * time.Minute".
func fold(s string) string {
	s = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`).Replace(strings.ToLower(s))
	var b strings.Builder
	prev := ""
	for _, word := range strings.Fields(s) {
		if prev != "" && !strings.ContainsAny(prev[len(prev)-1:], codePunct) &&
			!strings.ContainsAny(word[:1], codePunct) {
			b.WriteByte(' ')
		}
		b.WriteString(word)
		prev = word
	}
	return strings.NewReplacer(",)", ")", ",]", "]", ",}", "}").Replace(b.String())
}

// Annotate checks every rule node's requirement against its cited code,
// read through roots, with opts (see Check), and records the outcome in Extra["grounding"] (a Status) and, for an
// ungrounded rule, Extra["grounding_missing"] (Result.Summary). A rule whose
// citations cannot be read is StatusUnchecked; verify reports the broken
// citation itself. An ungrounded rule whose citation is merely too narrow
// is widened (see widening): the lines added to its Sources are recorded in
// Extra[graph.ExtraGroundingWidened].
func Annotate(roots extract.Roots, nodes []graph.Node, opts Options) []graph.Node {
	for i := range nodes {
		if nodes[i].Type != graph.TypeRule {
			continue
		}
		requirement := graph.RuleRequirement(nodes[i].Body)
		result := Result{Status: StatusUnchecked}
		if citations, err := Load(roots, nodes[i].Sources); err == nil {
			result = Check(requirement, citations, opts)
		}
		if added := widening(result, nodes[i].Sources); len(added) > 0 {
			sources := append(slices.Clone(nodes[i].Sources), added...)
			if citations, err := Load(roots, sources); err == nil {
				if widened := Check(
					requirement,
					citations,
					opts,
				); widened.Status == StatusGrounded {
					nodes[i].Sources, result = sources, widened
					nodes[i].Extra = withExtra(
						nodes[i].Extra,
						graph.ExtraGroundingWidened,
						refList(added),
					)
				}
			}
		}
		if nodes[i].Extra == nil {
			nodes[i].Extra = map[string]string{}
		}
		nodes[i].Extra["grounding"] = string(result.Status)
		if summary := result.Summary(); summary != "" {
			nodes[i].Extra["grounding_missing"] = summary
		}
	}
	return nodes
}

// maxWidenDistance is how far from a citation, in lines, a missing value may
// sit for the citation to be widened to it: the same function, give or take.
const maxWidenDistance = 30

// widening returns the lines that would ground an ungrounded rule whose
// citation is merely too narrow: each missing value occurs at exactly one
// other line of a cited file, within maxWidenDistance of one of the rule's
// citations, and is not a lone digit (which occurs everywhere). nil when
// any missing value falls short, so a widened rule is never a guess.
func widening(result Result, sources []extract.Ref) []extract.Ref {
	if result.Status != StatusUngrounded || slices.ContainsFunc(sources, rooted) {
		return nil
	}
	var added []extract.Ref
	for _, m := range result.Missing {
		if len(m.Elsewhere) != 1 || isLoneDigit(m.Literal) {
			return nil
		}
		file, line, ok := splitLocation(m.Elsewhere[0])
		if !ok || !nearCitation(file, line, sources) {
			return nil
		}
		ref := extract.Ref{Path: file, Lines: strconv.Itoa(line) + "-" + strconv.Itoa(line)}
		if !slices.Contains(added, ref) {
			added = append(added, ref)
		}
	}
	return added
}

// rooted reports whether a ref cites a tree other than --src: such rules
// are an extractor's, cited exactly, and are never widened.
func rooted(ref extract.Ref) bool { return ref.Root != "" }

// isLoneDigit reports whether a stated value is a single digit.
func isLoneDigit(literal string) bool {
	return len(literal) == 1 && literal[0] >= '0' && literal[0] <= '9'
}

// splitLocation parses a Miss location, "path:line".
func splitLocation(location string) (string, int, bool) {
	i := strings.LastIndexByte(location, ':')
	if i < 0 {
		return "", 0, false
	}
	line, err := strconv.Atoi(location[i+1:])
	return location[:i], line, err == nil
}

// nearCitation reports whether line of file is within maxWidenDistance of
// one of sources.
func nearCitation(file string, line int, sources []extract.Ref) bool {
	for _, s := range sources {
		a, b, err := extract.ParseLines(s.Lines)
		if err == nil && s.Path == file && line >= a-maxWidenDistance &&
			line <= b+maxWidenDistance {
			return true
		}
	}
	return false
}

// refList writes refs as "path:a-b" joined by ", ".
func refList(refs []extract.Ref) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.File() + ":" + r.Lines
	}
	return strings.Join(parts, ", ")
}

// withExtra sets extra[key] = value, allocating extra when nil.
func withExtra(extra map[string]string, key, value string) map[string]string {
	if extra == nil {
		extra = map[string]string{}
	}
	extra[key] = value
	return extra
}
