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
	"io/fs"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"codetospec/internal/extract"
	"codetospec/internal/graph"
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

// Load reads the cited ranges of refs from fsys (rooted at the source tree).
//
// Requires: every ref.Lines is "A-B" with 1 <= A <= B.
// Ensures:  one Citation per ref, in order; End is clamped to the file length.
func Load(fsys fs.FS, refs []extract.Ref) ([]Citation, error) {
	files := make(map[string][]string)
	citations := make([]Citation, 0, len(refs))
	for _, ref := range refs {
		start, end, err := extract.ParseLines(ref.Lines)
		if err != nil {
			return nil, fmt.Errorf("grounding.Load: %s: %w", ref.Path, err)
		}
		lines, ok := files[ref.Path]
		if !ok {
			data, readErr := fs.ReadFile(fsys, path.Clean(ref.Path))
			if readErr != nil {
				return nil, fmt.Errorf("grounding.Load: %w", readErr)
			}
			lines = strings.Split(string(data), "\n")
			files[ref.Path] = lines
		}
		citations = append(citations, Citation{
			Path:  ref.Path,
			Start: start,
			End:   min(end, len(lines)),
			File:  lines,
		})
	}
	return citations, nil
}

// Check verifies that every number and quoted string stated in requirement
// occurs in the cited lines.
//
// decimalSeparator is how the requirement's language writes decimals (see
// locale.Text.DecimalSeparator): with ',' a "1,000" reads 1.000, with '.' it
// reads one thousand.
//
// Requires: decimalSeparator is ',' or '.'; empty inputs yield StatusUnchecked.
// Ensures:  Status is StatusUnchecked iff requirement states no literal or
// no citation is given; otherwise Missing lists, in order of appearance, each
// literal not found in any cited range.
func Check(requirement string, citations []Citation, decimalSeparator byte) Result {
	literals := extractLiterals(requirement, decimalSeparator)
	if len(literals) == 0 || len(citations) == 0 {
		return Result{Status: StatusUnchecked}
	}
	var cited []string
	for _, c := range citations {
		cited = append(cited, c.File[c.Start-1:c.End]...)
	}
	result := Result{Status: StatusGrounded}
	for _, lit := range literals {
		if lit.foundIn(cited) {
			continue
		}
		result.Status = StatusUngrounded
		result.Missing = append(result.Missing, Miss{
			Literal:   lit.text,
			Elsewhere: elsewhere(lit, citations),
		})
	}
	return result
}

// elsewhere returns up to maxElsewhere "path:line" locations in the cited
// files, outside every cited range, whose line contains lit.
func elsewhere(lit literal, citations []Citation) []string {
	var locations []string
	seenFile := make(map[string]bool)
	for _, c := range citations {
		if seenFile[c.Path] {
			continue
		}
		seenFile[c.Path] = true
		for i, line := range c.File {
			n := i + 1
			if insideAny(c.Path, n, citations) || !lit.foundIn([]string{line}) {
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
			if m[g] >= 0 {
				found = append(found, located{m[0], quoted(text[m[0]:m[1]], text[m[g]:m[g+1]])})
			}
		}
	}
	for _, m := range claimSingleQuoted.FindAllStringSubmatchIndex(text, -1) {
		inner := text[m[2]:m[3]]
		spans = append(spans, m[2:4])
		found = append(found, located{m[2], quoted("'"+inner+"'", inner)})
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

func quoted(asWritten, inner string) literal {
	return literal{text: asWritten, folded: fold(inner)}
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

// foundIn reports whether lit occurs in any of lines.
func (l literal) foundIn(lines []string) bool {
	if !l.number {
		return strings.Contains(fold(strings.Join(lines, "\n")), l.folded)
	}
	for _, line := range lines {
		for _, tok := range codeNumber.FindAllString(line, -1) {
			v, err := strconv.ParseFloat(strings.ReplaceAll(tok, "_", ""), 64)
			if err != nil {
				continue
			}
			if sameNumber(v, l.value) || (l.percent && sameNumber(v*percentScale, l.value)) {
				return true
			}
		}
	}
	return false
}

// percentScale converts a ratio in code (0.1) to the percentage a
// requirement states (10 %).
const percentScale = 100

// sameNumber compares with a relative tolerance, so 0.1*100 equals 10.
func sameNumber(a, b float64) bool {
	const tolerance = 1e-9
	return math.Abs(a-b) <= tolerance*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// fold lowercases s, unifies typographic quotes and collapses whitespace, so
// a quoted string matches code regardless of case and layout.
func fold(s string) string {
	s = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`).Replace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

// Annotate checks every rule node's requirement against its cited code in
// fsys, reading numbers with decimalSeparator (see Check), and records the outcome in Extra["grounding"] (a Status) and, for an
// ungrounded rule, Extra["grounding_missing"] (Result.Summary). A rule whose
// citations cannot be read is StatusUnchecked; verify reports the broken
// citation itself.
func Annotate(fsys fs.FS, nodes []graph.Node, decimalSeparator byte) []graph.Node {
	for i := range nodes {
		if nodes[i].Type != graph.TypeRule {
			continue
		}
		result := Result{Status: StatusUnchecked}
		if citations, err := Load(fsys, nodes[i].Sources); err == nil {
			result = Check(graph.RuleRequirement(nodes[i].Body), citations, decimalSeparator)
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
