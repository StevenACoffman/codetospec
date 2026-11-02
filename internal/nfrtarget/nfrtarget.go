// Package nfrtarget reads a person's answers to the nfr_target questions
// (questions.KindNFRTarget): the target of a non-functional rule whose
// code states none. The answers live in <out>/targets.yaml, keyed by rule
// id, which codetospec never writes. A rule id survives runs and renames
// (graph.MatchRules), where a question id hashes the cited code: an edit
// of those lines would discard a target that still holds. Each answer
// records the digest of the code it was given for instead; when that code
// changes, the target is kept and a stale_target question asks for a look.
// Pure but for Load.
package nfrtarget

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
	"github.com/StevenACoffman/codetospec/internal/quality"
	"gopkg.in/yaml.v3"
)

// File is the human-owned file, in the output directory, that sets the
// targets by rule id.
const File = "targets.yaml"

// Kind is what kind of answer a target is.
type Kind string

// The kinds of target. A measurable quality gets a measure; one that does
// not reduce to a number (usability, a security practice) a policy a
// reviewer applies, or an example: a reference case to do no worse than.
const (
	KindMeasure Kind = "measure"
	KindPolicy  Kind = "policy"
	KindExample Kind = "example"
)

// Measure is a measurable target, in the terms of Planguage (Tom Gilb):
// the scale of what is measured, the meter that measures it, the level
// that must be reached (the fail threshold) and the level planned.
type Measure struct {
	Scale string `yaml:"scale"`
	Meter string `yaml:"meter"`
	Must  string `yaml:"must,omitempty"`
	Plan  string `yaml:"plan,omitempty"`
}

// Target is a person's answer for one rule: exactly one of Measure,
// Policy and Example, and the digest of the rule's cited code when it was
// given (its frontmatter digest).
type Target struct {
	Digest  string   `yaml:"digest"`
	Measure *Measure `yaml:"measure,omitempty"`
	Policy  string   `yaml:"policy,omitempty"`
	Example string   `yaml:"example,omitempty"`
}

// Kind is the kind of answer t is.
//
// Requires: t from Load (exactly one answer).
func (t *Target) Kind() Kind {
	switch {
	case t.Measure != nil:
		return KindMeasure
	case t.Policy != "":
		return KindPolicy
	default:
		return KindExample
	}
}

// Summary is t in one line: a measure's must and plan levels, or the
// policy or the example as written. It is the rule's threshold
// frontmatter.
//
// Requires: t from Load.
func (t *Target) Summary() string {
	switch t.Kind() {
	case KindMeasure:
		var levels []string
		if t.Measure.Must != "" {
			levels = append(levels, "must "+t.Measure.Must)
		}
		if t.Measure.Plan != "" {
			levels = append(levels, "plan "+t.Measure.Plan)
		}
		return strings.Join(levels, "; ")
	case KindPolicy:
		return t.Policy
	case KindExample:
		return t.Example
	}
	return ""
}

// Load reads the targets of outDir; an empty set when the file does not
// exist.
//
// Requires: nothing.
// Ensures:  every target is keyed by a rule id, has a digest and exactly
// one answer, and a measure has a scale, a meter and a must or plan level;
// otherwise an error naming the file and the entry, so a typo fails the
// run before any LLM call instead of leaving the question open.
func Load(outDir string) (map[string]Target, error) {
	path := filepath.Join(outDir, File)
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Target{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var targets map[string]Target
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err = decoder.Decode(&targets); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, id := range slices.Sorted(maps.Keys(targets)) {
		if problem := invalid(id, targets[id]); problem != "" {
			return nil, fmt.Errorf("%s: %s: %s", path, id, problem)
		}
	}
	if targets == nil {
		targets = map[string]Target{}
	}
	return targets, nil
}

// invalid says what is wrong with the target t of id; "" when nothing.
func invalid(id string, t Target) string {
	answers := 0
	for _, given := range []bool{t.Measure != nil, t.Policy != "", t.Example != ""} {
		if given {
			answers++
		}
	}
	switch {
	case !strings.HasPrefix(id, graph.TypeRule+"."):
		return "not a rule id (rule.<domain>.<slug>)"
	case t.Digest == "":
		return "a target needs the digest of the code it was set for (the rule's digest frontmatter)"
	case answers != 1:
		return "a target needs exactly one of measure, policy and example"
	case t.Measure != nil && (t.Measure.Scale == "" || t.Measure.Meter == ""):
		return "a measure needs a scale and a meter"
	case t.Measure != nil && t.Measure.Must == "" && t.Measure.Plan == "":
		return "a measure needs a must or a plan level"
	}
	return ""
}

// Annotate sets the target of each non-functional rule targets answers
// for: the threshold frontmatter becomes the target's summary, in place of
// one the code states (the person's target is the requirement), and
// ExtraTarget its kind; a target given for other code than the rule cites
// now also gets ExtraTargetStale, the digest it was given for. It returns
// the ids of the targets no non-functional rule of nodes takes.
//
// Requires: nodes stamped (graph.ExtraDigest set) and annotated by
// quality.AnnotateThresholds; targets from Load.
// Ensures:  nodes is not modified; unused sorted. A target of a rule that
// is gone, or no longer non-functional, is unused, not an error: the rule
// may come back.
func Annotate(nodes []graph.Node, targets map[string]Target) ([]graph.Node, []string) {
	out := slices.Clone(nodes)
	used := make(map[string]bool, len(targets))
	for i := range out {
		t, ok := targets[out[i].ID]
		if !ok || !quality.IsNFR(&out[i]) {
			continue
		}
		used[out[i].ID] = true
		out[i].Extra = maps.Clone(out[i].Extra)
		out[i].Extra[graph.ExtraThreshold] = t.Summary()
		out[i].Extra[graph.ExtraTarget] = string(t.Kind())
		if t.Digest != out[i].Extra[graph.ExtraDigest] {
			out[i].Extra[graph.ExtraTargetStale] = t.Digest
		}
	}
	var unused []string
	for _, id := range slices.Sorted(maps.Keys(targets)) {
		if !used[id] {
			unused = append(unused, id)
		}
	}
	return out, unused
}
