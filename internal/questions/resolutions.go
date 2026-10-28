package questions

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"gopkg.in/yaml.v3"
)

// ResolutionsFile is the human-owned file, in the output directory, that
// resolves questions by id.
const ResolutionsFile = "resolutions.yaml"

// Dispositions of a resolution.
const (
	// Verified: a person checked the finding and the rule is right as it
	// stands (the value is not invented, the citation is the right one).
	Verified = "verified"
	// OutOfScope: the finding is real but not worth acting on (generated
	// code, a domain no one specifies).
	OutOfScope = "out-of-scope"
)

// Resolution is a person's answer to a question.
type Resolution struct {
	Disposition string `json:"disposition" yaml:"disposition"`
	Reason      string `json:"reason"      yaml:"reason"`
}

// isID reports whether s has the form of a question id: "q-" and 12
// lower-case hex digits.
func isID(s string) bool {
	digits, found := strings.CutPrefix(s, "q-")
	return found && len(digits) == idDigits &&
		strings.Trim(digits, "0123456789abcdef") == ""
}

// Load reads the resolutions of outDir; an empty set when the file does
// not exist.
//
// Requires: nothing.
// Ensures:  every resolution has a question id, a known disposition and a
// reason; otherwise an error naming the file and the entry, so a typo
// fails the run instead of silently listing the question again.
func Load(outDir string) (map[string]Resolution, error) {
	path := filepath.Join(outDir, ResolutionsFile)
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Resolution{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var resolutions map[string]Resolution
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err = decoder.Decode(&resolutions); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, id := range slices.Sorted(maps.Keys(resolutions)) {
		r := resolutions[id]
		switch {
		case !isID(id):
			return nil, fmt.Errorf("%s: %q is not a question id (q- and 12 hex digits)", path, id)
		case r.Disposition != Verified && r.Disposition != OutOfScope:
			return nil, fmt.Errorf("%s: %s: disposition %q is not %s or %s",
				path, id, r.Disposition, Verified, OutOfScope)
		case r.Reason == "":
			return nil, fmt.Errorf("%s: %s: a resolution needs a reason", path, id)
		}
	}
	return resolutions, nil
}

// Ledger is the run's questions with their resolutions.
type Ledger struct {
	Questions []Question `json:"questions"` // sorted by id
	// Unused are the resolution ids no question of the run has, sorted:
	// the cited code changed since, or the finding is gone.
	Unused   []string        `json:"unused_resolutions"`
	resolved map[string]bool // the finding of each resolved question
}

// Apply attaches the resolutions to the questions they answer.
//
// Requires: questions from List; resolutions from Load.
// Ensures:  questions and resolutions are not modified; every resolution
// is attached to its question or listed in Unused.
func Apply(questions []Question, resolutions map[string]Resolution) Ledger {
	l := Ledger{Questions: slices.Clone(questions), Unused: []string{}, resolved: map[string]bool{}}
	used := map[string]bool{}
	for i := range l.Questions {
		q := &l.Questions[i]
		if r, ok := resolutions[q.ID]; ok {
			q.Resolution = &r
			used[q.ID] = true
			l.resolved[q.finding] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(resolutions)) {
		if !used[id] {
			l.Unused = append(l.Unused, id)
		}
	}
	return l
}

// ResolvedRule reports whether the question of kind about the rule id is
// resolved; a rule has one question of each kind at most.
func (l *Ledger) ResolvedRule(kind, id string) bool {
	return l.resolved[kind+"\x00"+id]
}

// ResolvedGap reports whether the question about a coverage gap is
// resolved.
func (l *Ledger) ResolvedGap(g *graph.CoverageGap) bool {
	return l.resolved[gapFinding(g)]
}

// Count is how many questions are resolved.
func (l *Ledger) Count() int {
	n := 0
	for i := range l.Questions {
		if l.Questions[i].Resolution != nil {
			n++
		}
	}
	return n
}
