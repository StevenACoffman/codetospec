package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/cover"
	"golang.org/x/tools/go/packages"
)

// Attributes of an executed fact: the lines of its file the profiled test
// run executed, and those it did not, as comma-separated "a-b" spans. A
// line of neither holds no statement (a comment, a declaration, a brace).
const (
	attrExecuted   = "executed"
	attrUnexecuted = "unexecuted"
)

// ExecutedFacts turns a coverage profile into one executed fact per file
// of the module: which lines the profiled test run executed, observed
// rather than derived, beside the test facts' static reach. A profile
// names a file by its package import path; files of other modules are
// skipped, and so are the files stale says changed since the profile was
// written (their lines no longer match the profile's).
//
// Requires: files maps "<import path>/<file name>" to the file's path
// relative to the module root (see profileFiles).
// Ensures:  sorted by id; a line both executed and not, by two blocks
// sharing it, counts as executed; skipped lists the stale files, sorted.
func ExecutedFacts(
	profiles []*cover.Profile,
	files map[string]string,
	stale func(rel string) bool,
) ([]Fact, []string) {
	var (
		facts   []Fact
		skipped []string
	)
	for _, p := range profiles {
		rel, known := files[p.FileName]
		if !known {
			continue
		}
		if stale(rel) {
			skipped = append(skipped, rel)
			continue
		}
		executed, unexecuted, last := map[int]bool{}, map[int]bool{}, 0
		for _, b := range p.Blocks {
			for line := b.StartLine; line <= b.EndLine; line++ {
				if b.Count > 0 {
					executed[line] = true
				} else {
					unexecuted[line] = true
				}
			}
			last = max(last, b.EndLine)
		}
		for line := range executed {
			delete(unexecuted, line)
		}
		facts = append(facts, Fact{
			Kind: "executed",
			ID:   "executed." + rel,
			Attrs: map[string]string{
				attrExecuted:   lineSpans(executed),
				attrUnexecuted: lineSpans(unexecuted),
			},
			Source:    Ref{Path: rel, Lines: "1-" + itoa(max(last, 1))},
			Origin:    origin,
			Certainty: certain,
		})
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	sort.Strings(skipped)
	return facts, skipped
}

// lineSpans writes a set of lines as sorted, comma-separated "a-b" spans
// of consecutive lines ("3-5,9-9").
func lineSpans(lines map[int]bool) string {
	sorted := make([]int, 0, len(lines))
	for line := range lines {
		sorted = append(sorted, line)
	}
	sort.Ints(sorted)
	var spans []string
	for i := 0; i < len(sorted); {
		j := i
		for j+1 < len(sorted) && sorted[j+1] == sorted[j]+1 {
			j++
		}
		spans = append(spans, itoa(sorted[i])+"-"+itoa(sorted[j]))
		i = j + 1
	}
	return strings.Join(spans, ",")
}

// profileFiles maps each non-test Go file of pkgs under root, by the name
// a coverage profile gives it ("<import path>/<file name>"), to its path
// relative to root.
func profileFiles(pkgs []*packages.Package, root string) map[string]string {
	files := map[string]string{}
	for _, p := range pkgs {
		for _, file := range p.GoFiles {
			if rel, err := relTo(root, file); err == nil && !strings.HasPrefix(rel, "..") {
				files[p.PkgPath+"/"+filepath.Base(file)] = rel
			}
		}
	}
	return files
}

// readProfile reads a coverage profile, and a test telling whether a file
// of root changed after it: modified later than the profile, or gone.
func readProfile(path, root string) ([]*cover.Profile, func(rel string) bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("coverage profile: %w", err)
	}
	profiles, err := cover.ParseProfiles(path)
	if err != nil {
		return nil, nil, fmt.Errorf("coverage profile %s: %w", path, err)
	}
	written := info.ModTime()
	stale := func(rel string) bool {
		file, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		return statErr != nil || file.ModTime().After(written)
	}
	return profiles, stale, nil
}
